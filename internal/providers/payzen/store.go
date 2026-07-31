// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import "sync"

// Store est le depot en memoire des transactions PayZen en cours. Il
// indexe par formToken (utilise cote marchand pour instancier le
// SmartForm) et par UUID (utilise cote controle par le meme marchand
// pour interroger le statut). Une meme Transaction se retrouve dans
// les deux maps — pas de duplication, juste deux pointeurs.
//
// Sans TTL ni tampon circulaire en phase 1 : le plafond de retention
// PAYSIM_MAX_PAYMENTS et la conception circulaire viennent en phase 4.
// Ici, un simple map protege par un mutex suffit — le simulateur
// tourne en une seule replique (invariant 8).
type Store struct {
	mu             sync.RWMutex
	byToken        map[string]*Transaction
	byUUID         map[string]*Transaction
	bySubscription map[string]*Subscription
}

// NewStore instancie un Store vide.
func NewStore() *Store {
	return &Store{
		byToken:        make(map[string]*Transaction),
		byUUID:         make(map[string]*Transaction),
		bySubscription: make(map[string]*Subscription),
	}
}

// Save indexe une transaction sous ses deux cles. Ecrase silencieusement
// une transaction existante sous le meme FormToken ou UUID — c'est ce
// qu'on veut pour les mises a jour (evolutions d'etat d'un meme paiement).
// Le pointeur passe reste la propriete du Store apres Save : l'appelant
// ne doit plus le modifier sans reprendre le verrou.
func (s *Store) Save(tx *Transaction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tx.FormToken != "" {
		s.byToken[tx.FormToken] = tx
	}
	if tx.UUID != "" {
		s.byUUID[tx.UUID] = tx
	}
}

// ByToken retourne la transaction associee a un formToken, ou nil si
// inconnue. Ne differencie pas "jamais existe" de "supprime" — le
// simulateur ne supprime pas en phase 1.
func (s *Store) ByToken(token string) *Transaction {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byToken[token]
}

// ByUUID retourne la transaction associee a un UUID, ou nil si inconnue.
func (s *Store) ByUUID(uuid string) *Transaction {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byUUID[uuid]
}

// Len retourne le nombre de transactions distinctes indexees. Utile
// pour l'observabilite et les tests. Une transaction indexee sur ses
// deux cles ne compte qu'une fois — on retourne max(byToken, byUUID).
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.byToken)
	if len(s.byUUID) > n {
		n = len(s.byUUID)
	}
	return n
}

// SaveSubscription indexe un abonnement par son ID. Ecrase silencieusement
// un abonnement existant sous le meme ID, comme Save pour Transaction.
func (s *Store) SaveSubscription(sub *Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.ID != "" {
		s.bySubscription[sub.ID] = sub
	}
}

// SubscriptionByID retourne l'abonnement associe a l'ID, ou nil si inconnu.
func (s *Store) SubscriptionByID(id string) *Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bySubscription[id]
}

// LenSubscriptions retourne le nombre d'abonnements indexes.
func (s *Store) LenSubscriptions() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bySubscription)
}
