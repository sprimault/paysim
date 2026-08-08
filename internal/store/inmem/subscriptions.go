// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package inmem

import (
	"sort"
	"sync"

	"github.com/sprimault/paysim/internal/store"
)

// SubscriptionsRepository est l'implémentation mémoire de
// store.SubscriptionRepository.
type SubscriptionsRepository struct {
	mu   sync.RWMutex
	subs map[string]store.SubscriptionRecord
}

// NewSubscriptionsRepository construit un dépôt vide.
func NewSubscriptionsRepository() *SubscriptionsRepository {
	return &SubscriptionsRepository{subs: make(map[string]store.SubscriptionRecord)}
}

// Save insère ou remplace, comme l'ON CONFLICT de la version SQLite.
func (r *SubscriptionsRepository) Save(rec *store.SubscriptionRecord) error {
	if rec == nil {
		return errNilRecord
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs[rec.ID] = *rec
	return nil
}

// ByID retourne l'abonnement, ou nil, nil si inconnu.
func (r *SubscriptionsRepository) ByID(id string) (*store.SubscriptionRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.subs[id]
	if !ok {
		return nil, nil
	}
	return &rec, nil
}

// ByProvider retourne les abonnements d'un provider, plus récemment
// modifié d'abord — la version SQLite trie sur updated_at, et non
// created_at comme les moyens de paiement.
func (r *SubscriptionsRepository) ByProvider(provider string) ([]*store.SubscriptionRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*store.SubscriptionRecord, 0, len(r.subs))
	for _, rec := range r.subs {
		if rec.Provider == provider {
			cp := rec
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// Count retourne le nombre d'abonnements, tous providers confondus.
func (r *SubscriptionsRepository) Count() (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.subs), nil
}

// DeleteByID supprime un abonnement. Sans erreur sur un identifiant
// inconnu : le DELETE SQLite n'en signale pas non plus.
func (r *SubscriptionsRepository) DeleteByID(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subs, id)
	return nil
}

// DeleteByProvider supprime les abonnements d'un provider et retourne
// le nombre supprimé.
func (r *SubscriptionsRepository) DeleteByProvider(provider string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for id, rec := range r.subs {
		if rec.Provider == provider {
			delete(r.subs, id)
			n++
		}
	}
	return n, nil
}

// DeleteAll purge et retourne le nombre supprimé.
func (r *SubscriptionsRepository) DeleteAll() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.subs)
	r.subs = make(map[string]store.SubscriptionRecord)
	return n, nil
}

// Cancel marque l'abonnement annulé. Idempotent, et sans erreur sur un
// identifiant inconnu — symétrique de Revoke côté moyens de paiement.
func (r *SubscriptionsRepository) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.subs[id]
	if !ok {
		return nil
	}
	rec.Cancelled = true
	r.subs[id] = rec
	return nil
}

// Close ne libère rien — la mémoire est rendue avec le processus.
func (r *SubscriptionsRepository) Close() error { return nil }
