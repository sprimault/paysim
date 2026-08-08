// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package inmem implémente les dépôts de store en mémoire.
//
// Il existe parce que le mode mémoire — le défaut de l'image, et ce que
// rencontre quiconque lance un `docker run` sans configuration — ne
// disposait d'aucun dépôt : l'API répondait « liste vide » et « 404 »
// sur des objets pourtant créés et utilisables. Un intégrateur en
// concluait que son enrôlement avait échoué alors que tout marchait.
//
// La règle que ce paquet doit tenir est une équivalence, pas une
// ressemblance : à séquence identique, ces dépôts répondent comme leurs
// homologues SQLite. Les choix qui en découlent — ordre de tri, upsert,
// copies défensives — sont copiés sur eux, pas décidés ici.
package inmem

import (
	"sort"
	"sync"

	"github.com/sprimault/paysim/internal/store"
)

// PaymentMethodsRepository est l'implémentation mémoire de
// store.PaymentMethodRepository.
//
// Aucun plafond, à l'image de SQLite : borner les moyens ferait
// disparaître un alias auquel un abonnement se réfère encore, et
// l'échéance suivante échouerait sans que rien ne l'explique. Le plafond
// PAYSIM_MAX_PAYMENTS ne concerne que les paiements.
type PaymentMethodsRepository struct {
	mu      sync.RWMutex
	methods map[string]store.PaymentMethodRecord
}

// NewPaymentMethodsRepository construit un dépôt vide.
func NewPaymentMethodsRepository() *PaymentMethodsRepository {
	return &PaymentMethodsRepository{methods: make(map[string]store.PaymentMethodRecord)}
}

// Save insère ou remplace, comme l'ON CONFLICT de la version SQLite.
//
// Le record est copié : le stocker par pointeur laisserait l'appelant
// modifier après coup ce que le dépôt est censé conserver, une
// divergence que SQLite ne peut pas avoir puisqu'il sérialise.
func (r *PaymentMethodsRepository) Save(rec *store.PaymentMethodRecord) error {
	if rec == nil {
		return errNilRecord
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods[rec.Token] = *rec
	return nil
}

// ByToken retourne le moyen, ou nil, nil si inconnu — même contrat que
// SQLite, où l'absence de ligne n'est pas une erreur.
func (r *PaymentMethodsRepository) ByToken(token string) (*store.PaymentMethodRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.methods[token]
	if !ok {
		return nil, nil
	}
	return &rec, nil
}

// ByProvider retourne les moyens d'un provider, plus récent d'abord.
//
// Le tri secondaire par token n'a pas d'équivalent SQL : il départage
// deux enregistrements créés dans la même nanoseconde, cas courant en
// mémoire et introuvable sur disque. Sans lui, l'ordre dépendrait du
// parcours de map, et deux appels successifs ne s'accorderaient pas.
func (r *PaymentMethodsRepository) ByProvider(provider string) ([]*store.PaymentMethodRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*store.PaymentMethodRecord, 0, len(r.methods))
	for _, rec := range r.methods {
		if rec.Provider == provider {
			cp := rec
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Token < out[j].Token
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Revoke marque le moyen révoqué. Idempotent, et sans erreur sur un
// token inconnu : l'UPDATE SQLite n'en signale pas non plus.
func (r *PaymentMethodsRepository) Revoke(token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.methods[token]
	if !ok {
		return nil
	}
	rec.Revoked = true
	r.methods[token] = rec
	return nil
}

// Count retourne le nombre de moyens, tous providers confondus.
func (r *PaymentMethodsRepository) Count() (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.methods), nil
}

// DeleteAll purge et retourne le nombre supprimé.
func (r *PaymentMethodsRepository) DeleteAll() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.methods)
	r.methods = make(map[string]store.PaymentMethodRecord)
	return n, nil
}

// Close ne libère rien — la mémoire est rendue avec le processus.
func (r *PaymentMethodsRepository) Close() error { return nil }
