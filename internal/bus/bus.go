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
	"sync"
	"sync/atomic"
	"time"
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
	ID   uint64
	Type string
	At   time.Time
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
}

// New instancie un Bus prêt à l'emploi.
func New() *Bus {
	return &Bus{
		subs:   make(map[chan Event]struct{}),
		buffer: make([]Event, 0, bufferCap),
	}
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
	defer b.bufMu.Unlock()
	if len(b.buffer) == 0 {
		return nil, 0
	}
	highWater := b.buffer[len(b.buffer)-1].ID
	out := make([]Event, 0, len(b.buffer))
	for _, e := range b.buffer {
		if e.ID > lastID {
			out = append(out, e)
		}
	}
	return out, highWater
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
