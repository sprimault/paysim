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

	"github.com/sprimault/paysim/internal/bus"
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

// historySize borne l'historique circulaire des webhooks. 200 couvre
// largement l'usage interactif ; passer au-delà nécessiterait un
// stockage persistant (phase 4).
const historySize = 200

// WebhookRecord est une entrée d'historique : le webhook enqueue plus
// le résultat effectif de la tentative. Consommé par l'API UI pour
// afficher le journal de livraison avec bouton de rejeu.
type WebhookRecord struct {
	Webhook     Webhook
	Status      string    // "delivered" | "failed"
	StatusCode  int       // status HTTP reçu, 0 si erreur avant réponse
	ErrorMsg    string    // message si failed, sinon vide
	CompletedAt time.Time // instant fin de tentative
}

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

	histMu   sync.RWMutex
	history  [historySize]WebhookRecord
	histIdx  int // prochaine position d'écriture
	histFull bool

	publisher *bus.Bus // optionnel — publie webhook_delivered/failed
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
		// Notifier l'UI en temps réel : un webhook "pending" apparaît
		// immédiatement dans la timeline avant même sa livraison.
		// Payload allégé — headers et body seront récupérés via
		// GET /webhooks/{id} si le front en a besoin.
		q.publisher.Publish(bus.Event{
			Type: "webhook_enqueued",
			At:   w.CreatedAt,
			Data: map[string]any{
				"id":        w.ID,
				"url":       w.URL,
				"attempts":  w.Attempts,
				"createdAt": w.CreatedAt,
			},
		})
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

// SetPublisher branche un bus d'événements sur la queue. À chaque
// tentative de livraison, un event bus.Event est publié :
// "webhook_delivered" (2xx) ou "webhook_failed" (autre / erreur).
// Optionnel — sans publisher, la queue fonctionne inchangée.
func (q *Queue) SetPublisher(b *bus.Bus) {
	q.publisher = b
}

// Recent retourne les n derniers enregistrements (les plus récents
// d'abord). Si n dépasse la capacité de l'historique ou le nombre
// d'entrées, tout est retourné. Snapshot indépendant — l'appelant
// peut modifier le slice sans affecter le buffer interne.
func (q *Queue) Recent(n int) []WebhookRecord {
	q.histMu.RLock()
	defer q.histMu.RUnlock()

	total := q.histIdx
	if q.histFull {
		total = historySize
	}
	if n > total {
		n = total
	}
	if n <= 0 {
		return nil
	}

	out := make([]WebhookRecord, n)
	// Parcours à l'envers depuis histIdx-1 (le plus récent).
	for i := 0; i < n; i++ {
		pos := (q.histIdx - 1 - i + historySize) % historySize
		out[i] = q.history[pos]
	}
	return out
}

// recordHistory ajoute une entrée à l'anneau circulaire. Appelée en
// fin de deliver().
func (q *Queue) recordHistory(rec WebhookRecord) {
	q.histMu.Lock()
	q.history[q.histIdx] = rec
	q.histIdx = (q.histIdx + 1) % historySize
	if q.histIdx == 0 {
		q.histFull = true
	}
	q.histMu.Unlock()
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
			q.finish(w, "failed", 0, "delay cancelled")
			return
		}
	}

	w.Attempts++
	w.LastTryAt = time.Now().UTC()

	req, err := http.NewRequest(http.MethodPost, w.URL, bytes.NewReader(w.Body))
	if err != nil {
		q.failed.Add(1)
		q.logger.Error("webhook_failed", "id", w.ID, "url", w.URL, "err", err)
		q.finish(w, "failed", 0, err.Error())
		return
	}
	for k, v := range w.Headers {
		req.Header.Set(k, v)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		q.failed.Add(1)
		q.logger.Error("webhook_failed", "id", w.ID, "url", w.URL, "err", err)
		q.finish(w, "failed", 0, err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		q.delivered.Add(1)
		q.logger.Info("webhook_delivered",
			"id", w.ID, "url", w.URL,
			"status", resp.StatusCode, "attempts", w.Attempts)
		q.finish(w, "delivered", resp.StatusCode, "")
	} else {
		q.failed.Add(1)
		q.logger.Warn("webhook_failed",
			"id", w.ID, "url", w.URL,
			"status", resp.StatusCode, "attempts", w.Attempts)
		q.finish(w, "failed", resp.StatusCode, "")
	}
}

// finish enregistre l'historique et publie l'événement bus. Extrait
// pour concentrer les side-effects post-livraison.
func (q *Queue) finish(w Webhook, status string, statusCode int, errMsg string) {
	rec := WebhookRecord{
		Webhook:     w,
		Status:      status,
		StatusCode:  statusCode,
		ErrorMsg:    errMsg,
		CompletedAt: time.Now().UTC(),
	}
	q.recordHistory(rec)

	q.publisher.Publish(bus.Event{
		Type: "webhook_" + status,
		At:   rec.CompletedAt,
		Data: rec,
	})
}
