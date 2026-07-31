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
package bus

import (
	"sync"
	"time"
)

// Event est l'unité de fan-out. Type discriminant sur string plate,
// Data reste any (le broker ne sérialise pas — c'est le rôle du
// consommateur SSE).
//
// Types conventionnels utilisés dans Paysim :
//   - payment_created
//   - payment_state_changed
//   - webhook_delivered
//   - webhook_failed
type Event struct {
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
}

// New instancie un Bus prêt à l'emploi.
func New() *Bus {
	return &Bus{subs: make(map[chan Event]struct{})}
}

// Publish diffuse l'événement à tous les abonnés en cours. Envoi
// non-bloquant : si le channel d'un abonné est plein, l'événement est
// perdu pour lui (pas pour les autres). Récepteur nil safe — un
// producteur qui reçoit un *Bus nil se comporte comme si personne
// n'écoutait.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
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
