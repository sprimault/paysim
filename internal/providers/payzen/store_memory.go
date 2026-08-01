// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import "sync"

// MemoryStore est l'implémentation en mémoire de Store. Les
// transactions vivent dans deux maps (indexées par FormToken et par
// UUID), les abonnements dans une troisième. Sans TTL ni tampon
// circulaire — l'invariant 8 (une seule réplique) permet de rester
// simple. Le plafond de rétention PAYSIM_MAX_PAYMENTS est appliqué
// côté queue delivery, pas ici.
//
// Aucune erreur possible : toutes les méthodes retournent nil pour
// l'error du contrat Store. Signature conservée pour la symétrie
// avec SQLiteStore.
type MemoryStore struct {
	mu             sync.RWMutex
	byToken        map[string]*Transaction
	byUUID         map[string]*Transaction
	bySubscription map[string]*Subscription
}

// NewMemoryStore instancie un MemoryStore vide.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byToken:        make(map[string]*Transaction),
		byUUID:         make(map[string]*Transaction),
		bySubscription: make(map[string]*Subscription),
	}
}

// Save indexe une transaction sous ses deux clés. Le pointeur passé
// reste la propriété du Store après Save — l'appelant ne doit plus
// le modifier sans reprendre le verrou.
func (s *MemoryStore) Save(tx *Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tx.FormToken != "" {
		s.byToken[tx.FormToken] = tx
	}
	if tx.UUID != "" {
		s.byUUID[tx.UUID] = tx
	}
	return nil
}

// ByToken retourne la transaction associée à un formToken, ou nil
// si inconnue.
func (s *MemoryStore) ByToken(token string) (*Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byToken[token], nil
}

// ByUUID retourne la transaction associée à un UUID, ou nil si
// inconnue.
func (s *MemoryStore) ByUUID(uuid string) (*Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byUUID[uuid], nil
}

// Len retourne le nombre de transactions distinctes indexées. Une
// transaction indexée sur ses deux clés ne compte qu'une fois — on
// retourne max(byToken, byUUID).
func (s *MemoryStore) Len() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.byToken)
	if len(s.byUUID) > n {
		n = len(s.byUUID)
	}
	return n, nil
}

// AllTransactions retourne un snapshot de toutes les transactions
// indexées par UUID. Snapshot pris sous verrou puis relâché —
// l'appelant reçoit ses propres pointeurs, mais la Transaction
// pointe toujours vers le même domain.Payment vivant.
func (s *MemoryStore) AllTransactions() ([]*Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Transaction, 0, len(s.byUUID))
	for _, tx := range s.byUUID {
		out = append(out, tx)
	}
	return out, nil
}

// SaveSubscription indexe un abonnement par son ID.
func (s *MemoryStore) SaveSubscription(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.ID != "" {
		s.bySubscription[sub.ID] = sub
	}
	return nil
}

// SubscriptionByID retourne l'abonnement associé à l'ID, ou nil si
// inconnu.
func (s *MemoryStore) SubscriptionByID(id string) (*Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bySubscription[id], nil
}

// LenSubscriptions retourne le nombre d'abonnements indexés.
func (s *MemoryStore) LenSubscriptions() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bySubscription), nil
}

// Delete retire une transaction de ses deux index (byToken + byUUID).
// Idempotent — l'UUID inconnu n'est pas une erreur.
func (s *MemoryStore) Delete(uuid string) error {
	if uuid == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.byUUID[uuid]
	if tx == nil {
		return nil
	}
	delete(s.byUUID, uuid)
	if tx.FormToken != "" {
		delete(s.byToken, tx.FormToken)
	}
	return nil
}

// DeleteAllTransactions vide les deux maps de transactions. Ne touche
// pas aux abonnements.
func (s *MemoryStore) DeleteAllTransactions() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.byUUID)
	s.byUUID = make(map[string]*Transaction)
	s.byToken = make(map[string]*Transaction)
	return n, nil
}

// Close est un no-op pour MemoryStore — implémenté pour respecter
// le contrat Store.
func (s *MemoryStore) Close() error {
	return nil
}
