// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
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
// pilotée par un unique scheduler via Run — le scheduler lance chaque
// delivery en goroutine indépendante pour supporter les délais
// différenciés (out-of-order chaos) sans bloquer les livraisons
// suivantes.  Sûre à Enqueue depuis plusieurs goroutines concurrentes.
type Queue struct {
	client *http.Client
	logger *slog.Logger
	jobs   chan Webhook

	delivered atomic.Int64
	failed    atomic.Int64
	running   atomic.Bool
	inflight  sync.WaitGroup // livraisons en cours (goroutines lancées, non terminées)
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
			// puis on attend les livraisons en vol (delays en cours,
			// requêtes HTTP en cours). Sans ça, un webhook avec Delay
			// serait perdu à l'arrêt.
			for {
				select {
				case w := <-q.jobs:
					q.launch(ctx, w)
				default:
					q.inflight.Wait()
					return nil
				}
			}
		case w := <-q.jobs:
			q.launch(ctx, w)
		}
	}
}

// launch démarre une goroutine de livraison avec suivi WaitGroup.
// Chaque delivery est indépendante — supporte des délais différenciés
// (out-of-order chaos) sans bloquer les livraisons suivantes.
func (q *Queue) launch(ctx context.Context, w Webhook) {
	q.inflight.Add(1)
	go func() {
		defer q.inflight.Done()
		q.deliver(ctx, w)
	}()
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

// deliver traite un webhook : respecte le Delay éventuel, construit
// la requête, l'envoie via le client HTTP injecté, met à jour les
// compteurs et logue le résultat. Une tentative unique — les retry
// (avec backoff, jitter, idempotency) arrivent plus tard.
//
// Reçoit le Webhook par valeur : les compteurs Attempts et LastTryAt
// mis à jour ici ne sont visibles qu'au sein de ce log ; le Webhook
// original a déjà été consommé du channel et n'est plus référencé.
//
// Si le contexte est annulé pendant le Delay, la livraison est
// abandonnée (comptée en Failed avec log). C'est ce qui permet un
// arrêt de la queue en un temps borné même quand des délais sont
// pendants.
func (q *Queue) deliver(ctx context.Context, w Webhook) {
	if w.Delay > 0 {
		select {
		case <-time.After(w.Delay):
		case <-ctx.Done():
			q.failed.Add(1)
			q.logger.Warn("webhook_delay_cancelled",
				"id", w.ID, "url", w.URL, "delay", w.Delay)
			return
		}
	}

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
