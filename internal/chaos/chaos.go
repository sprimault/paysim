// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package chaos centralise l'injection de pannes contrôlées dans Paysim.
// Deux injecteurs en phase 2 v1 : latence artificielle et taux d'erreur
// 5xx. Deux voies d'activation : configuration statique
// (PAYSIM_CHAOS_LATENCY_MS, PAYSIM_CHAOS_ERROR_RATE) et valeurs magiques
// sur les montants. L'activation par header per-request (v2) et le chaos
// sur les webhooks (v3) viendront ensuite.
//
// Invariant central : le chaos est **inerte par défaut**. Une struct
// Chaos nil est valide et se comporte comme aucun chaos ; une struct
// non-nil avec Config zéro se comporte de même. Aucun défaut caché.
package chaos

import (
	"context"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sprimault/paysim/internal/format"
)

// Config regroupe les paramètres d'activation du chaos. Toute valeur
// zéro rend l'injecteur correspondant inerte — l'invariant 5 tient
// naturellement si la config est vide.
type Config struct {
	// LatencyMs est le délai ajouté à chaque requête HTTP traitée
	// par le middleware. Zéro = pas de latence injectée.
	LatencyMs int

	// ErrorRate est le pourcentage (0-100) de requêtes qui reçoivent
	// une réponse 500 injectée par le middleware. Zéro = pas d'erreur.
	// Une valeur supérieure à 100 est plafonnée à 100.
	ErrorRate int
}

// Chaos porte l'état d'injection. Injecté en pointeur dans les
// consommateurs — un nil signifie « aucun chaos ». Les méthodes
// tolèrent le récepteur nil pour permettre `chaos.Middleware(...)` dans
// une composition inconditionnelle sans nil-check à chaque site d'appel.
type Chaos struct {
	cfg    Config
	logger *slog.Logger
	rng    *rand.Rand
	mu     sync.Mutex // rand.Rand n'est pas thread-safe
}

// New instancie un Chaos avec la config donnée. Un ErrorRate négatif
// est ramené à 0, un ErrorRate > 100 est plafonné à 100.
func New(cfg Config, logger *slog.Logger) *Chaos {
	if cfg.ErrorRate < 0 {
		cfg.ErrorRate = 0
	}
	if cfg.ErrorRate > 100 {
		cfg.ErrorRate = 100
	}
	if cfg.LatencyMs < 0 {
		cfg.LatencyMs = 0
	}
	return &Chaos{
		cfg:    cfg,
		logger: logger,
		// #nosec G404 -- pas de crypto ici, juste de la simulation
		// non-cryptographique de taux d'erreur. math/rand suffit.
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Sleep applique la latence configurée + une éventuelle latence extra
// (utilisée pour les magic amounts). Annulable via ctx pour ne pas
// bloquer l'arrêt du serveur. Récepteur nil safe.
func (c *Chaos) Sleep(ctx context.Context, extraMs int) {
	if c == nil {
		if extraMs > 0 {
			select {
			case <-time.After(time.Duration(extraMs) * time.Millisecond):
			case <-ctx.Done():
			}
		}
		return
	}
	total := c.cfg.LatencyMs + extraMs
	if total <= 0 {
		return
	}
	select {
	case <-time.After(time.Duration(total) * time.Millisecond):
	case <-ctx.Done():
	}
}

// ShouldFail retourne true si l'ErrorRate déclenche une injection 5xx
// pour cette requête. Récepteur nil safe : retourne toujours false.
func (c *Chaos) ShouldFail() bool {
	if c == nil || c.cfg.ErrorRate <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rng.Intn(100) < c.cfg.ErrorRate
}

// Overrides regroupe les directives extraites du header X-Paysim-Chaos.
// Champs à zéro = pas de directive pour cette dimension.
type Overrides struct {
	// LatencyMs impose un délai en millisecondes sur cette requête.
	LatencyMs int

	// Status force ce code HTTP en retour (400 <= Status < 600).
	// Zéro = pas de status forcé.
	Status int
}

// ParseHeader décode la valeur d'un header X-Paysim-Chaos en Overrides.
// Format : query-string style, ex "latency=2000&status=500". Parsing
// best-effort : chaque token invalide est silencieusement ignoré — un
// header mal formé ne casse pas la requête, il est juste inactif.
//
// Clés reconnues : "latency" (int > 0, ms), "status" (int dans 400-599).
func ParseHeader(h string) Overrides {
	var o Overrides
	for _, kv := range strings.Split(h, "&") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "latency":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				o.LatencyMs = n
			}
		case "status":
			if n, err := strconv.Atoi(val); err == nil && n >= 400 && n < 600 {
				o.Status = n
			}
		}
	}
	return o
}

// Middleware retourne un http.Handler qui applique le chaos avant de
// passer à next. Toujours actif — même sur récepteur nil — parce qu'un
// header X-Paysim-Chaos doit pouvoir déclencher du chaos même sans
// config statique configurée.
//
// Précédence : le header X-Paysim-Chaos, quand présent, remplace
// intégralement la config statique. Cohérent avec l'intention d'un
// testeur qui vise une requête précise — pas de mélange subtil qui
// masquerait ce qu'il vient de configurer via header.
//
// À appliquer sur les routes qui simulent le PSP (/api-payment/V4/*),
// pas sur l'API de contrôle (/paysim/simulate/*) ni sur les probes.
func (c *Chaos) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Header per-request prime sur config statique.
		if headerVal := r.Header.Get("X-Paysim-Chaos"); headerVal != "" {
			o := ParseHeader(headerVal)
			if o.LatencyMs > 0 {
				select {
				case <-time.After(time.Duration(o.LatencyMs) * time.Millisecond):
				case <-r.Context().Done():
					return
				}
			}
			if o.Status > 0 {
				if c != nil {
					c.logger.Info("chaos_header_status_injected",
						"path", r.URL.Path, "status", o.Status)
				}
				http.Error(w, "chaos: injected via header", o.Status)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		// 2. Config statique (si Chaos non nil).
		if c == nil {
			next.ServeHTTP(w, r)
			return
		}
		c.Sleep(r.Context(), 0)
		if c.ShouldFail() {
			c.logger.Info("chaos_error_injected", "path", r.URL.Path)
			http.Error(w, "chaos: injected 500", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MagicOutcome inspecte un montant et retourne un outcome forcé si le
// montant porte une valeur magique. Retourne "" si aucune magic value
// détectée. Convention initiale phase 2 v1 :
//
//   - centimes terminant par 01 → outcome "UNPAID" (refus systématique).
//
// D'autres magic values (02 → 3DS échoue, cartes spéciales) arrivent
// en phase 2 v2. Ajout non-breaking pour les consommateurs.
func MagicOutcome(amount format.Amount) string {
	if amount%100 == 1 {
		return "UNPAID"
	}
	return ""
}

// MagicLatencyMs retourne un délai extra en millisecondes si le
// montant porte une valeur magique de latence. Convention initiale :
//
//   - centimes terminant par 03 → latence 30000 ms (provoque un
//     timeout côté client marchand sans changer le mode de sortie).
//
// Zéro si aucune magic value détectée.
func MagicLatencyMs(amount format.Amount) int {
	if amount%100 == 3 {
		return 30_000
	}
	return 0
}

// declinedTestPANs est la liste fermée des numéros de carte de test
// que Paysim reconnaît comme refus systématique au rejeu par token
// (`charge_token`). Un PAN d'exactement cette valeur → outcome UNPAID
// automatique, sans montant magique ni révocation manuelle nécessaire.
//
// Ces PANs sont **Luhn-valides** et respectent les préfixes de BIN
// standard par marque, pour rester générables par un script client
// (Cadensio) sans effort particulier et sans risquer de confondre avec
// une CB réelle en test. Le complément du préfixe est composé de zéros
// et se termine par le check digit Luhn correspondant.
//
// Périmètre : la reconnaissance n'agit **qu'au rejeu** (charge_token),
// pas au premier paiement. Pour un refus au parcours utilisateur
// (simulate), utiliser un montant magique (voir MagicOutcome).
// Cf. docs/testing-cards.md pour l'usage complet côté intégrateur.
var declinedTestPANs = map[string]bool{
	"4000000000000002": true, // Visa (préfixe 400000, 16 chiffres)
	"5105105105105100": true, // Mastercard (préfixe 510510, 16 chiffres)
	"2223000000000007": true, // Mastercard série 2 (préfixe 222300, 16 chiffres)
	"378282000000008":  true, // Amex (préfixe 378282, 15 chiffres)
}

// IsDeclinedTestPAN retourne true si le PAN est un numéro de test
// réservé pour provoquer un refus systématique au rejeu.
func IsDeclinedTestPAN(pan string) bool { return declinedTestPANs[pan] }
