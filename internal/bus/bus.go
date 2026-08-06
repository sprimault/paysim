// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package bus est un fan-out non-bloquant d'événements applicatifs.
// Sert principalement à alimenter les flux SSE de l'interface : les
// producteurs (delivery, payzen) publient un Event, chaque abonné (un
// client SSE = une goroutine) le reçoit dans son channel.
//
// Non-bloquant par contrat : si un abonné ne consomme pas assez vite,
// son channel est plein, l'événement est PERDU pour lui — jamais
// pour les autres. C'est le compromis correct pour une UI de test :
// mieux vaut rater un rafraîchissement qu'engorger tout le processus.
//
// Le bus attribue un ID monotone à chaque événement et garde les N
// derniers dans un ring buffer, ce qui permet à un client SSE qui
// reprend contact (via header Last-Event-ID standard EventSource) de
// rattraper ce qu'il a manqué pendant sa déconnexion — voir
// SnapshotSince et l'handler api.streamEvents.
package bus

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

// bufferCap borne le ring buffer des events récents. Dimensionné large
// (10 000) pour couvrir des déconnexions SSE de plusieurs minutes sur
// une charge normale de simulateur. Au-delà, un client qui revient
// avec un Last-Event-ID trop ancien perdra des events — le front doit
// alors refetch un snapshot complet via les endpoints REST.
const bufferCap = 10_000

// Event est l'unité de fan-out.
//
// L'ID est monotone et assigné par le Bus au moment du Publish —
// l'appelant ne le fixe pas. Il sert au replay via Last-Event-ID.
//
// Type discriminant sur string plate, Data reste any (le broker ne
// sérialise pas — c'est le rôle du consommateur SSE).
//
// Types conventionnels utilisés dans Paysim :
//   - payment_created
//   - payment_state_changed
//   - webhook_enqueued
//   - webhook_delivered
//   - webhook_failed
type Event struct {
	// ID est un compteur monotone attribué à la publication. Il permet
	// à un client SSE reconnecté de reprendre où il s'était arrêté,
	// sans trou ni doublon.
	ID uint64

	// Type nomme l'événement — payment_created, payment_state_changed,
	// webhook_enqueued, reset…
	Type string

	// At est l'instant de publication, en UTC.
	At time.Time

	// Data porte la charge utile, sérialisée en JSON vers les abonnés.
	// Volontairement typée any : le bus ne connaît pas les domaines
	// qu'il transporte.
	Data any
}

// Bus est un fan-out N vers M en mémoire. Sûr en concurrence.
// Récepteur nil safe pour Publish : simplifie l'injection optionnelle
// côté producteurs — un bus non configuré n'oblige pas à un nil-check
// à chaque site d'appel.
type Bus struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}

	counter atomic.Uint64

	bufMu  sync.Mutex
	buffer []Event

	// Persistance optionnelle. Activée via WithPersistence — sinon
	// nil et Publish reste 100% en mémoire. Le worker draine
	// persistQueue en tâche de fond, avec un drop non-bloquant si
	// saturation (correspond au contrat général du bus : mieux vaut
	// perdre un event que bloquer un producteur).
	persistRepo   store.EventRepository
	persistLogger *slog.Logger
	persistQueue  chan Event
	persistDone   chan struct{}
	persistOnce   sync.Once
}

// New instancie un Bus prêt à l'emploi.
func New() *Bus {
	return &Bus{
		subs:   make(map[chan Event]struct{}),
		buffer: make([]Event, 0, bufferCap),
	}
}

// WithPersistence active la persistance des events sur un
// EventRepository. Un worker goroutine drains persistQueue et
// persiste chaque event de manière asynchrone — le publisher reste
// non-bloquant. Un logger optionnel permet de tracer les échecs
// d'écriture (nil = pas de log).
//
// SnapshotSince consultera automatiquement le repository si le ring
// buffer mémoire ne couvre plus le catch-up demandé (client SSE qui
// rattrape après un long down).
//
// À appeler une seule fois après New et avant tout Publish. Un
// deuxième appel est un no-op.
func (b *Bus) WithPersistence(repo store.EventRepository, logger *slog.Logger) *Bus {
	if b == nil || repo == nil {
		return b
	}
	b.persistOnce.Do(func() {
		b.persistRepo = repo
		b.persistLogger = logger
		b.persistQueue = make(chan Event, 1024)
		b.persistDone = make(chan struct{})
		go b.persistWorker()
	})
	return b
}

// Close ferme la persistance : plus aucun Publish n'est enfilé après
// l'appel, le worker draine la queue et sort. Idempotent, nil-safe.
// Ne ferme PAS le repository — l'appelant garde la propriété
// (typiquement cmd/paysim/main.go qui fermera aussi le DB).
func (b *Bus) Close() error {
	if b == nil || b.persistQueue == nil {
		return nil
	}
	// Signaler la fermeture au worker et attendre son drainage.
	close(b.persistQueue)
	<-b.persistDone
	return nil
}

// persistWorker draine persistQueue et persiste chaque event.
// Erreurs loguées mais non fatales — un event manquant en base
// dégrade le catch-up post-restart, mais ne casse pas le bus vivant.
func (b *Bus) persistWorker() {
	defer close(b.persistDone)
	for evt := range b.persistQueue {
		dataJSON, err := json.Marshal(evt.Data)
		if err != nil {
			b.logf("bus_persist_marshal_failed", "id", evt.ID, "err", err)
			continue
		}
		if err := b.persistRepo.Save(store.EventRecord{
			ID:       evt.ID,
			Type:     evt.Type,
			At:       evt.At,
			DataJSON: string(dataJSON),
		}); err != nil {
			b.logf("bus_persist_save_failed", "id", evt.ID, "err", err)
		}
	}
}

func (b *Bus) logf(msg string, args ...any) {
	if b.persistLogger == nil {
		return
	}
	b.persistLogger.Error(msg, args...)
}

// Publish diffuse l'événement à tous les abonnés en cours. L'ID est
// assigné ici — l'appelant peut laisser e.ID à zéro, il sera écrasé.
//
// Envoi non-bloquant : si le channel d'un abonné est plein,
// l'événement est perdu pour lui (pas pour les autres). Récepteur nil
// safe — un producteur qui reçoit un *Bus nil se comporte comme si
// personne n'écoutait.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	e.ID = b.counter.Add(1)

	// Le ring buffer garde les bufferCap derniers events. Un simple
	// append + trim de tête vaut la peine ici : bufferCap est petit
	// par rapport à la mémoire, et la copie liée au trim reste
	// négligeable devant un Publish (qui écrit dans N channels).
	b.bufMu.Lock()
	b.buffer = append(b.buffer, e)
	if len(b.buffer) > bufferCap {
		b.buffer = b.buffer[len(b.buffer)-bufferCap:]
	}
	b.bufMu.Unlock()

	// Persistance async si activée. Drop non-bloquant si la queue est
	// pleine — cohérent avec le contrat général du bus (perdre un
	// event vaut mieux que bloquer un producteur).
	if b.persistQueue != nil {
		select {
		case b.persistQueue <- e:
		default:
			b.logf("bus_persist_queue_full", "id", e.ID)
		}
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			// Channel plein — abonné à la traîne, drop cet événement
			// pour lui uniquement. Ne bloque pas les autres.
		}
	}
}

// Subscribe ouvre un abonnement avec un buffer donné. Retourne le
// channel de lecture et une fonction d'annulation qui ferme le
// channel et retire l'abonnement du bus. À appeler dans un defer par
// le handler SSE pour libérer proprement à la déconnexion client.
//
// bufSize < 1 est ramené à 1 — un channel non-bufferisé rendrait
// tous les Publish bloquants et casserait le contrat non-bloquant.
func (b *Bus) Subscribe(bufSize int) (<-chan Event, func()) {
	if bufSize < 1 {
		bufSize = 1
	}
	ch := make(chan Event, bufSize)

	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	// sync.Once rend unsub idempotent — un appel repété (ex. defer +
	// appel manuel) ne cause pas de "close of closed channel".
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, unsub
}

// SnapshotSince retourne, en une prise atomique, la liste des events
// du buffer avec un ID strictement supérieur à lastID, plus le plus
// grand ID actuellement en buffer (highWater).
//
// Le contrat est conçu pour composer avec Subscribe : la séquence
// canonique côté handler SSE est
//   ch, unsub := bus.Subscribe(N)
//   defer unsub()
//   events, high := bus.SnapshotSince(lastID)
//   for _, e := range events { write(e) }        // catch-up
//   for e := range ch { if e.ID > high { write(e) } }  // live, filtré
//
// Cet ordre garantit qu'un event publié entre Subscribe et Snapshot
// est reçu deux fois par le channel/snapshot, mais dédupliqué par
// le filtre `e.ID > high` — ni trou, ni doublon.
//
// Récepteur nil safe.
func (b *Bus) SnapshotSince(lastID uint64) ([]Event, uint64) {
	if b == nil {
		return nil, 0
	}
	b.bufMu.Lock()
	bufferCopy := make([]Event, len(b.buffer))
	copy(bufferCopy, b.buffer)
	b.bufMu.Unlock()

	if len(bufferCopy) == 0 {
		// Ring vide — pas de high water à annoncer. Si la persistance
		// est active, on peut quand même récupérer les events post-
		// lastID depuis la base (utile juste après un restart avant
		// tout Publish).
		if b.persistRepo != nil {
			return b.snapshotFromRepo(lastID)
		}
		return nil, 0
	}

	oldestBuffered := bufferCopy[0].ID
	highWater := bufferCopy[len(bufferCopy)-1].ID

	// Filtrer le ring pour ne renvoyer que ce qui est > lastID.
	fromBuffer := make([]Event, 0, len(bufferCopy))
	for _, e := range bufferCopy {
		if e.ID > lastID {
			fromBuffer = append(fromBuffer, e)
		}
	}

	// Si le lastID demandé est plus ancien que le ring buffer et
	// que la persistance est active, aller chercher le trou en base.
	if b.persistRepo != nil && lastID+1 < oldestBuffered {
		olderThanBuffer, err := b.loadFromRepo(lastID, oldestBuffered)
		if err == nil && len(olderThanBuffer) > 0 {
			// Concat : événements DB (plus anciens) + événements ring.
			return append(olderThanBuffer, fromBuffer...), highWater
		}
	}
	return fromBuffer, highWater
}

// snapshotFromRepo est le fallback pur base : appelé quand le ring
// est vide (post-restart avant tout Publish).
func (b *Bus) snapshotFromRepo(lastID uint64) ([]Event, uint64) {
	recs, err := b.persistRepo.Since(lastID)
	if err != nil || len(recs) == 0 {
		return nil, 0
	}
	out := make([]Event, 0, len(recs))
	var highWater uint64
	for _, r := range recs {
		out = append(out, Event{
			ID:   r.ID,
			Type: r.Type,
			At:   r.At,
			Data: parseData(r.DataJSON),
		})
		if r.ID > highWater {
			highWater = r.ID
		}
	}
	return out, highWater
}

// loadFromRepo récupère les events entre lastID (exclusif) et
// oldestBuffered (exclusif) — le trou qui manque au ring.
func (b *Bus) loadFromRepo(lastID, oldestBuffered uint64) ([]Event, error) {
	recs, err := b.persistRepo.Since(lastID)
	if err != nil {
		return nil, err
	}
	// Ne garder que ceux < oldestBuffered (le ring couvre le reste).
	out := make([]Event, 0, len(recs))
	for _, r := range recs {
		if r.ID >= oldestBuffered {
			break
		}
		out = append(out, Event{
			ID:   r.ID,
			Type: r.Type,
			At:   r.At,
			Data: parseData(r.DataJSON),
		})
	}
	return out, nil
}

// parseData désérialise le blob JSON en map[string]any. Perte de type
// fort acceptée — le catch-up SSE renvoie du JSON générique côté
// front, qui n'a pas besoin des struct Go d'origine.
func parseData(dataJSON string) any {
	if dataJSON == "" || dataJSON == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(dataJSON), &v); err != nil {
		return dataJSON
	}
	return v
}

// Subscribers retourne le nombre d'abonnés actifs. Utile pour
// l'observabilité et les tests.
func (b *Bus) Subscribers() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
