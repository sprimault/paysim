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

// Middleware retourne un http.Handler qui applique le chaos avant de
// passer à next : latence, puis éventuellement 500 injecté. Récepteur
// nil safe : renvoie next inchangé. À appliquer sur les routes qui
// simulent le PSP (/api-payment/V4/*), pas sur l'API de contrôle
// (/paysim/simulate/*) ni sur les probes de santé.
func (c *Chaos) Middleware(next http.Handler) http.Handler {
	if c == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
