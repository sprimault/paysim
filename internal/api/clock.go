// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/providers/payzen"
)

// ClockState décrit où en est l'horloge de l'instance.
type ClockState struct {
	// Now est l'heure que voit le simulateur, décalage compris. C'est
	// elle qui horodate les événements et les webhooks, pas l'heure du
	// serveur.
	Now time.Time `json:"now"`

	// Offset est le décalage cumulé, au format Go ("96h0m0s"). Zéro
	// signifie que l'instance est à l'heure réelle.
	Offset string `json:"offset"`

	// OffsetSeconds redonne le même décalage en secondes, pour les
	// appelants qui ne savent pas lire un format Go.
	OffsetSeconds float64 `json:"offsetSeconds"`
}

// AdvanceRequest demande de déplacer l'horloge.
type AdvanceRequest struct {
	// Duration est une durée Go : "96h", "45m", "3h30m". Les avances se
	// cumulent.
	Duration string `json:"duration"`
}

// horlogeIndisponible répond quand l'instance n'a pas d'horloge
// pilotable. Ne devrait pas arriver — main.go en fournit toujours une —
// mais mieux vaut une erreur explicite qu'un panic ou, pire, une route
// qui répond comme si elle avait avancé quelque chose.
func (h *Handler) horlogeIndisponible(w http.ResponseWriter) bool {
	if h.clock != nil {
		return false
	}
	h.logger.Error("api_clock_absente")
	http.Error(w, "horloge non configuree", http.StatusInternalServerError)
	return true
}

// now est l'heure du simulateur, pour tout ce que l'API horodate.
//
// Repli sur l'heure réelle quand l'horloge est absente : seuls les
// tests construisent un Handler sans, et une instance de production qui
// en manquerait le dirait déjà bruyamment — les trois routes /clock
// répondent 500. L'absence est donc détectée ailleurs, ce repli ne la
// masque pas.
func (h *Handler) now() time.Time {
	if h.clock == nil {
		return time.Now().UTC()
	}
	return h.clock.Now()
}

func (h *Handler) etatHorloge() ClockState {
	offset := h.clock.Offset()
	return ClockState{
		Now:           h.clock.Now(),
		Offset:        offset.String(),
		OffsetSeconds: offset.Seconds(),
	}
}

// getClock expose l'heure du simulateur et son décalage.
func (h *Handler) getClock(w http.ResponseWriter, _ *http.Request) {
	if h.horlogeIndisponible(w) {
		return
	}
	writeJSON(w, http.StatusOK, h.etatHorloge())
}

// advanceClock déplace l'horloge vers l'avant.
//
// Le recul est refusé : il produirait un paiement modifié avant d'avoir
// été créé, et un journal d'événements qui remonte le temps. Un
// simulateur qui ment sur la chronologie ne sert plus à rien — pour
// revenir en arrière, il y a reset.
func (h *Handler) advanceClock(w http.ResponseWriter, r *http.Request) {
	if h.horlogeIndisponible(w) {
		return
	}
	var req AdvanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d, err := time.ParseDuration(req.Duration)
	if err != nil {
		http.Error(w, "duration invalide : attendu une duree Go, ex. \"96h\"", http.StatusBadRequest)
		return
	}
	if d < 0 {
		http.Error(w, "duration negative : le temps ne recule pas, utiliser reset", http.StatusBadRequest)
		return
	}
	h.clock.Advance(d)
	etat := h.etatHorloge()
	h.logger.Info("horloge avancee", "duration", d.String(), "offset", etat.Offset)
	h.annoncerHorloge(etat)
	writeJSON(w, http.StatusOK, etat)
}

// resetClock ramène l'instance à l'heure réelle. Idempotent.
func (h *Handler) resetClock(w http.ResponseWriter, _ *http.Request) {
	if h.horlogeIndisponible(w) {
		return
	}
	h.clock.Reset()
	h.logger.Info("horloge reinitialisee")
	etat := h.etatHorloge()
	h.annoncerHorloge(etat)
	writeJSON(w, http.StatusOK, etat)
}

// annoncerHorloge publie le nouvel état sur le bus.
//
// Sans annonce, une instance avancée par curl, par un scénario ou
// depuis un autre onglet laisse les interfaces déjà ouvertes sur des
// données périmées, et leur bandeau ambre éteint alors que l'instance
// est décalée. Ce n'est pas cosmétique : l'exploitabilité d'un alias
// est calculée à la lecture, depuis l'horloge du serveur.
//
// Publié aussi quand le décalage est déjà nul. L'idempotence de reset
// ne rend pas l'annonce inutile — un client qui vient de se connecter
// n'a rien vu passer.
func (h *Handler) annoncerHorloge(etat ClockState) {
	h.publisher.Publish(bus.Event{
		Type: "clock_changed",
		At:   h.now(),
		Data: map[string]any{
			"now":           etat.Now,
			"offset":        etat.Offset,
			"offsetSeconds": etat.OffsetSeconds,
		},
	})
}

// parMarquesLyra concatène le résultat d'une lecture par provider sur
// les cinq marques de l'adaptateur Lyra, puis réordonne le tout.
//
// Les dépôts filtrent par un provider unique ; l'adaptateur en possède
// cinq. Sans cette boucle, une liste ne montrerait que les paiements
// PayZen et tairait les autres — silencieusement, « aucun résultat »
// étant une réponse plausible.
//
// Le comparateur n'est pas optionnel, et c'est le sujet : chaque dépôt
// rend déjà sa lecture triée, mais concaténer cinq listes triées produit
// une liste groupée par marque, pas une liste triée. L'ordre se perd
// donc exactement là où on croit ne rien faire, et l'oubli est invisible
// sur une instance mono-marque — quatre lectures sur cinq rendent vide.
// Exiger le comparateur à l'appel est ce qui empêche de le redécouvrir.
func parMarquesLyra[T any](lire func(string) ([]T, error), ordonner func(a, b T) int) ([]T, error) {
	var out []T
	for _, marque := range payzen.MarquesLyra {
		recs, err := lire(marque)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	slices.SortStableFunc(out, ordonner)
	return out, nil
}
