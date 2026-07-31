// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"sync"
	"testing"
)

func TestStoreSaveAndByToken(t *testing.T) {
	t.Parallel()
	s := NewStore()
	tx := &Transaction{FormToken: "tok-1", UUID: "uuid-1", OrderID: "order-1"}

	s.Save(tx)

	got := s.ByToken("tok-1")
	if got == nil {
		t.Fatal("ByToken(tok-1) = nil, veut la transaction")
	}
	if got.OrderID != "order-1" {
		t.Errorf("OrderID = %q, veut order-1", got.OrderID)
	}
}

func TestStoreSaveAndByUUID(t *testing.T) {
	t.Parallel()
	s := NewStore()
	tx := &Transaction{FormToken: "tok-1", UUID: "uuid-1", OrderID: "order-1"}

	s.Save(tx)

	got := s.ByUUID("uuid-1")
	if got == nil || got.OrderID != "order-1" {
		t.Errorf("ByUUID = %+v, veut la transaction avec OrderID=order-1", got)
	}
}

func TestStoreUnknownReturnsNil(t *testing.T) {
	t.Parallel()
	s := NewStore()
	if got := s.ByToken("inconnu"); got != nil {
		t.Errorf("ByToken(inconnu) = %+v, veut nil", got)
	}
	if got := s.ByUUID("inconnu"); got != nil {
		t.Errorf("ByUUID(inconnu) = %+v, veut nil", got)
	}
}

func TestStoreSaveOverwrites(t *testing.T) {
	t.Parallel()
	// Save deux fois avec meme FormToken doit ecraser — comportement
	// voulu pour les mises a jour d'etat.
	s := NewStore()
	s.Save(&Transaction{FormToken: "tok", UUID: "uuid", OrderID: "v1"})
	s.Save(&Transaction{FormToken: "tok", UUID: "uuid", OrderID: "v2"})

	if got := s.ByToken("tok"); got == nil || got.OrderID != "v2" {
		t.Errorf("apres reecriture : ByToken.OrderID = %v, veut v2", got)
	}
}

func TestStoreSaveHandlesEmptyKeys(t *testing.T) {
	t.Parallel()
	// Un Transaction sans FormToken (ou sans UUID) ne doit pas
	// polluer l'index correspondant — utile si on veut stocker un
	// contexte partiel en cours de construction.
	s := NewStore()
	s.Save(&Transaction{FormToken: "tok", UUID: "", OrderID: "no-uuid"})
	if got := s.ByUUID(""); got != nil {
		t.Errorf("ByUUID(chaine vide) = %+v, veut nil", got)
	}
	if got := s.ByToken("tok"); got == nil {
		t.Error("ByToken(tok) = nil, veut la transaction")
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	t.Parallel()
	// Plusieurs producteurs et lecteurs concurrents — le detecteur
	// de course (-race) doit passer.
	s := NewStore()
	const writers = 10
	const per = 100

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				token := "tok-" + string(rune('a'+w)) + "-" + string(rune('0'+i%10))
				s.Save(&Transaction{FormToken: token, UUID: token + "-u"})
			}
		}(w)
	}
	for r := 0; r < writers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				_ = s.ByToken("tok-a-0")
				_ = s.ByUUID("tok-a-0-u")
			}
		}()
	}
	wg.Wait()

	if s.Len() == 0 {
		t.Error("Len() = 0 apres writes concurrents")
	}
}
