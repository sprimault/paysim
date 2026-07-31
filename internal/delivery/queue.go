// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// Sentinelles exportées : l'appelant doit pouvoir distinguer une file
// pleine (défaut de dimensionnement) d'un mauvais usage (Run appelé
// deux fois).
var (
	// ErrQueueFull indique que la file est pleine et refuse un nouvel
	// Enqueue. Signal de saturation — soit la capacity est trop basse,
	// soit le worker n'arrive pas à suivre le rythme d'entrée.
	ErrQueueFull = errors.New("file de livraison pleine")

	// ErrAlreadyRunning indique un second appel à Run alors qu'un
	// worker tourne déjà. Une Queue ne se pilote que par un unique
	// worker — cohérent avec l'invariant 8 (une seule réplique).
	ErrAlreadyRunning = errors.New("worker deja en cours")
)

// Queue est la file de livraison. Une unique instance par processus,
// pilotée par un unique worker via Run. Sûre à Enqueue depuis plusieurs
// goroutines concurrentes (la synchronisation est portée par le channel
// interne et les compteurs atomiques).
type Queue struct {
	client *http.Client
	logger *slog.Logger
	jobs   chan Webhook

	delivered atomic.Int64
	failed    atomic.Int64
	running   atomic.Bool
}

// Stats est un instantané des compteurs. Utile pour l'observabilité,
// l'API de contrôle (phase 3) et les tests.
type Stats struct {
	Pending   int
	Delivered int
	Failed    int
}

// New instancie une Queue. Le client HTTP et le logger sont injectés :
// on veut pouvoir les substituer en tests (httptest.Client, logger
// discard) sans monkey-patch de globales.
//
// Une capacity < 1 est ramenée à 1 — une file de zéro n'a pas de sens
// et provoquerait un Enqueue toujours bloquant sans jamais démarrer.
func New(client *http.Client, logger *slog.Logger, capacity int) *Queue {
	if capacity < 1 {
		capacity = 1
	}
	return &Queue{
		client: client,
		logger: logger,
		jobs:   make(chan Webhook, capacity),
	}
}

// Enqueue pousse un webhook dans la file. Non-bloquant : retourne
// ErrQueueFull si la capacité est atteinte plutôt que d'attendre.
// Attendre bloquerait l'adaptateur appelant sur du chemin critique,
// ce qu'on ne veut pas — mieux vaut signaler la saturation à l'appelant
// et le laisser décider (log, retry, échec).
func (q *Queue) Enqueue(w Webhook) error {
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	select {
	case q.jobs <- w:
		return nil
	default:
		q.logger.Warn("queue_full", "id", w.ID, "url", w.URL)
		return ErrQueueFull
	}
}

// Run est la boucle du worker : livre les webhooks jusqu'à annulation
// du contexte. À l'annulation, draine la file (les jobs déjà présents
// sont traités) avant de sortir avec nil — c'est ce qui rend l'arrêt
// sur SIGTERM propre plutôt qu'approximatif. Bloquant.
//
// Un unique appelant est autorisé : un second Run concurrent retourne
// ErrAlreadyRunning sans démarrer de goroutine parasite.
func (q *Queue) Run(ctx context.Context) error {
	if !q.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer q.running.Store(false)

	for {
		select {
		case <-ctx.Done():
			// Drain avant sortie : on épuise les jobs déjà enqueuvés
			// avant la détection du ctx.Done(). Les Enqueue qui
			// arriveraient après ne sont pas garantis d'être traités.
			for {
				select {
				case w := <-q.jobs:
					q.deliver(w)
				default:
					return nil
				}
			}
		case w := <-q.jobs:
			q.deliver(w)
		}
	}
}

// Stats retourne un instantané des compteurs. Lecture atomique, sans
// verrou : la valeur peut être un peu périmée d'une livraison si
// consultée en plein milieu du traitement, ce qui est acceptable pour
// de l'observabilité.
func (q *Queue) Stats() Stats {
	return Stats{
		Pending:   len(q.jobs),
		Delivered: int(q.delivered.Load()),
		Failed:    int(q.failed.Load()),
	}
}

// deliver traite un webhook : construit la requête, l'envoie via le
// client HTTP injecté, met à jour les compteurs et logue le résultat.
// Une tentative unique en phase 1 — les retry (avec backoff, jitter,
// idempotency) arrivent avec le chaos en phase 2.
//
// Reçoit le Webhook par valeur : les compteurs Attempts et LastTryAt
// mis à jour ici ne sont visibles qu'au sein de ce log ; le Webhook
// original a déjà été consommé du channel et n'est plus référencé.
func (q *Queue) deliver(w Webhook) {
	w.Attempts++
	w.LastTryAt = time.Now().UTC()

	req, err := http.NewRequest(http.MethodPost, w.URL, bytes.NewReader(w.Body))
	if err != nil {
		q.failed.Add(1)
		q.logger.Error("webhook_failed", "id", w.ID, "url", w.URL, "err", err)
		return
	}
	for k, v := range w.Headers {
		req.Header.Set(k, v)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		q.failed.Add(1)
		q.logger.Error("webhook_failed", "id", w.ID, "url", w.URL, "err", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		q.delivered.Add(1)
		q.logger.Info("webhook_delivered",
			"id", w.ID, "url", w.URL,
			"status", resp.StatusCode, "attempts", w.Attempts)
	} else {
		q.failed.Add(1)
		q.logger.Warn("webhook_failed",
			"id", w.ID, "url", w.URL,
			"status", resp.StatusCode, "attempts", w.Attempts)
	}
}
