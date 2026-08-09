// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package inmem

import (
	"errors"
	"sort"
	"sync"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/store"
)

// PaymentsRepository est l'implémentation mémoire de
// store.PaymentRepository.
//
// Les événements vivent dans le record, là où SQLite leur consacre une
// table jointe : c'est le seul endroit où les deux implémentations
// divergent de structure, et cette divergence n'est pas observable —
// elle ne change ni ce qui est lu, ni dans quel ordre.
type PaymentsRepository struct {
	mu       sync.RWMutex
	payments map[string]store.PaymentRecord
}

// NewPaymentsRepository construit un dépôt vide.
func NewPaymentsRepository() *PaymentsRepository {
	return &PaymentsRepository{payments: make(map[string]store.PaymentRecord)}
}

// copyRecord isole le record du monde extérieur. Le slice d'événements
// doit être recopié explicitement : le copier par affectation ne
// duplique que l'en-tête, et les deux copies partageraient le même
// tableau — un appelant qui ajoute un événement modifierait alors ce que
// le dépôt conserve, ce que SQLite ne peut pas faire.
func copyRecord(rec store.PaymentRecord) store.PaymentRecord {
	if rec.Events != nil {
		events := make([]domain.Event, len(rec.Events))
		copy(events, rec.Events)
		rec.Events = events
	}
	return rec
}

// Save insère ou remplace, en préservant CreatedAt comme le fait
// l'ON CONFLICT SQLite : la date de création d'un paiement ne se
// réécrit pas à chaque transition d'état.
func (r *PaymentsRepository) Save(rec *store.PaymentRecord) error {
	if rec == nil {
		return errNilRecord
	}
	// Clé primaire vide : refusée, comme en SQLite. Sans cette garde,
	// toutes les entrées sans UUID s'écraseraient mutuellement sous la
	// clé "" et seraient retrouvées par une recherche à vide.
	if rec.UUID == "" {
		return errors.New("Save: UUID vide")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := copyRecord(*rec)
	if existing, ok := r.payments[rec.UUID]; ok {
		cp.CreatedAt = existing.CreatedAt
	}
	r.payments[rec.UUID] = cp
	return nil
}

// ByUUID retourne le paiement, ou nil, nil si inconnu.
func (r *PaymentsRepository) ByUUID(uuid string) (*store.PaymentRecord, error) {
	if uuid == "" {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.payments[uuid]
	if !ok {
		return nil, nil
	}
	cp := copyRecord(rec)
	return &cp, nil
}

// ByProviderRef retrouve un paiement par la référence que lui a donnée
// son adaptateur — formToken côté PayZen.
func (r *PaymentsRepository) ByProviderRef(provider, providerRef string) (*store.PaymentRecord, error) {
	if providerRef == "" {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rec := range r.payments {
		if rec.Provider == provider && rec.ProviderRef == providerRef {
			cp := copyRecord(rec)
			return &cp, nil
		}
	}
	return nil, nil
}

// All retourne tous les paiements, plus récemment modifié d'abord.
func (r *PaymentsRepository) All() ([]*store.PaymentRecord, error) {
	return r.filter(func(store.PaymentRecord) bool { return true }), nil
}

// ByProvider restreint All à un adaptateur.
func (r *PaymentsRepository) ByProvider(provider string) ([]*store.PaymentRecord, error) {
	return r.filter(func(rec store.PaymentRecord) bool { return rec.Provider == provider }), nil
}

// filter applique le tri de scanMany : updated_at décroissant, départagé
// par UUID quand deux paiements portent le même instant — fréquent en
// mémoire, où plusieurs écritures tiennent dans la même nanoseconde.
func (r *PaymentsRepository) filter(keep func(store.PaymentRecord) bool) []*store.PaymentRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*store.PaymentRecord, 0, len(r.payments))
	for _, rec := range r.payments {
		if keep(rec) {
			cp := copyRecord(rec)
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UUID < out[j].UUID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// Count retourne le nombre de paiements, tous providers confondus.
func (r *PaymentsRepository) Count() (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.payments), nil
}

// DeleteByUUID supprime un paiement, sans erreur s'il est inconnu.
func (r *PaymentsRepository) DeleteByUUID(uuid string) error {
	if uuid == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.payments, uuid)
	return nil
}

// DeleteByProvider supprime les paiements d'un adaptateur et retourne
// le nombre supprimé.
func (r *PaymentsRepository) DeleteByProvider(provider string) (int, error) {
	// Un provider vide ne purge rien : sans cette garde, l'appel
	// supprimerait les entrées dont le provider n'est pas renseigné,
	// ce que SQLite refuse de faire.
	if provider == "" {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for uuid, rec := range r.payments {
		if rec.Provider == provider {
			delete(r.payments, uuid)
			n++
		}
	}
	return n, nil
}

// DeleteAll purge et retourne le nombre supprimé.
func (r *PaymentsRepository) DeleteAll() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.payments)
	r.payments = make(map[string]store.PaymentRecord)
	return n, nil
}

// Close ne libère rien — la mémoire est rendue avec le processus.
func (r *PaymentsRepository) Close() error { return nil }
