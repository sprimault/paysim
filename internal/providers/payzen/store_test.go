// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"sync"
	"testing"

	"github.com/sprimault/paysim/internal/clock"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
	"github.com/sprimault/paysim/internal/store/inmem"
)

// newMemStore monte un Store adosse a des depots en memoire, tel que
// cmd/paysim le fait pour PAYSIM_STORE=memory.
//
// Remplace NewMemoryStore, supprime : le mode memoire ne passe plus par
// une implementation distincte du contrat, mais par le meme RepoStore
// que SQLite. Tester une implementation que la production n'emprunte
// plus est precisement ce qui a laisse passer le defaut de la v0.6.1.
func newMemStore() Store {
	return NewRepoStore(
		clock.System{},
		inmem.NewPaymentsRepository(0, nil),
		inmem.NewSubscriptionsRepository(),
		inmem.NewPaymentMethodsRepository(),
	)
}

// tx construit une transaction persistable.
//
// Le Payment n'est pas un ornement : une transaction sans etat de
// domaine n'a pas de sens, et Save la refuse. Ces tests l'omettaient et
// jetaient l'erreur de Save — ils passaient sur l'ancien MemoryStore,
// qui stockait la coquille telle quelle. Les deux implementations
// divergeaient donc sur ce point sans que rien ne le signale.
func tx(t *testing.T, formToken, uuid, orderID string) *Transaction {
	t.Helper()
	p, err := domain.New(clock.System{}, uuid, 1500, "EUR")
	if err != nil {
		t.Fatalf("domain.New(clock.System{}, %q) : %v", uuid, err)
	}
	return &Transaction{
		FormToken: formToken,
		UUID:      uuid,
		OrderID:   orderID,
		Amount:    1500,
		Currency:  "EUR",
		Payment:   p,
	}
}

func TestStoreSaveAndByToken(t *testing.T) {
	t.Parallel()
	s := newMemStore()

	if err := s.Save(tx(t, "tok-1", "uuid-1", "order-1")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.ByToken("tok-1")
	if err != nil {
		t.Fatalf("ByToken: %v", err)
	}
	if got == nil {
		t.Fatal("ByToken(tok-1) = nil, veut la transaction")
	}
	if got.OrderID != "order-1" {
		t.Errorf("OrderID = %q, veut order-1", got.OrderID)
	}
}

func TestStoreSaveAndByUUID(t *testing.T) {
	t.Parallel()
	s := newMemStore()

	if err := s.Save(tx(t, "tok-1", "uuid-1", "order-1")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.ByUUID("uuid-1")
	if err != nil {
		t.Fatalf("ByUUID: %v", err)
	}
	if got == nil || got.OrderID != "order-1" {
		t.Errorf("ByUUID = %+v, veut la transaction avec OrderID=order-1", got)
	}
}

func TestStoreUnknownReturnsNil(t *testing.T) {
	t.Parallel()
	s := newMemStore()
	// Une cle inconnue n'est pas une erreur : elle rend nil, nil. Les
	// appelants distinguent « absent » de « en panne » sur ce contrat.
	if got, err := s.ByToken("inconnu"); err != nil || got != nil {
		t.Errorf("ByToken(inconnu) = %+v, %v — veut nil, nil", got, err)
	}
	if got, err := s.ByUUID("inconnu"); err != nil || got != nil {
		t.Errorf("ByUUID(inconnu) = %+v, %v — veut nil, nil", got, err)
	}
}

func TestStoreSaveOverwrites(t *testing.T) {
	t.Parallel()
	// Deux Save sur la meme cle : le second ecrase, comportement voulu
	// pour les mises a jour d'etat successives d'un meme paiement.
	s := newMemStore()
	if err := s.Save(tx(t, "tok", "uuid", "v1")); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	if err := s.Save(tx(t, "tok", "uuid", "v2")); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	got, _ := s.ByToken("tok")
	if got == nil || got.OrderID != "v2" {
		t.Errorf("apres reecriture : ByToken = %+v, veut OrderID v2", got)
	}
	if n, _ := s.Len(); n != 1 {
		t.Errorf("Len = %d, veut 1 — l'ecrasement ne doit pas dupliquer", n)
	}
}

// TestStoreSaveRefuseSansPaiement fige la divergence decouverte en
// supprimant MemoryStore : celui-ci acceptait une transaction sans etat
// de domaine, RepoStore la refuse. Le meme appel reussissait donc en
// memoire et echouait en SQLite. Une seule implementation subsiste, et
// ce test verrouille son verdict.
func TestStoreSaveRefuseSansPaiement(t *testing.T) {
	t.Parallel()
	s := newMemStore()

	err := s.Save(&Transaction{FormToken: "tok", UUID: "uuid", OrderID: "sans-paiement"})
	if err == nil {
		t.Fatal("Save d'une transaction sans Payment = nil, veut une erreur")
	}
	if got, _ := s.ByToken("tok"); got != nil {
		t.Errorf("ByToken apres un Save refuse = %+v, veut nil", got)
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	t.Parallel()
	// Plusieurs producteurs et lecteurs concurrents — le detecteur
	// de course (-race) doit passer.
	s := newMemStore()
	const writers = 10
	const per = 100

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				token := "tok-" + string(rune('a'+w)) + "-" + string(rune('0'+i%10))
				p, err := domain.New(clock.System{}, token+"-u", 1500, "EUR")
				if err != nil {
					continue
				}
				_ = s.Save(&Transaction{
					FormToken: token,
					UUID:      token + "-u",
					Amount:    1500,
					Currency:  "EUR",
					Payment:   p,
				})
			}
		}(w)
	}
	for r := 0; r < writers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				_, _ = s.ByToken("tok-a-0")
				_, _ = s.ByUUID("tok-a-0-u")
			}
		}()
	}
	wg.Wait()

	if n, _ := s.Len(); n == 0 {
		t.Error("Len() = 0 apres writes concurrents")
	}
}

// TestRepoStore_marquesLyra couvre le cœur du multi-marque : un paiement
// étiqueté systempay doit rester visible de l'adaptateur, qui filtrait
// auparavant sur la seule valeur « payzen ».
//
// Sans ce test, un paiement d'une autre marque disparaîtrait des listes,
// de la relecture par formToken et du compte — silencieusement, puisque
// « aucun résultat » est une réponse plausible.
func TestRepoStore_marquesLyra(t *testing.T) {
	t.Parallel()
	s := newMemStore()

	for i, marque := range MarquesLyra {
		p, err := domain.New(clock.System{}, "u-"+marque, 1000, "EUR")
		if err != nil {
			t.Fatalf("domain.New : %v", err)
		}
		if err := s.Save(&Transaction{
			FormToken: "tok-" + marque,
			UUID:      "u-" + marque,
			Brand:     marque,
			OrderID:   "CMD-" + marque,
			Amount:    format.Amount(1000 + i),
			Currency:  "EUR",
			Payment:   p,
		}); err != nil {
			t.Fatalf("Save %s : %v", marque, err)
		}
	}

	n, err := s.Len()
	if err != nil {
		t.Fatalf("Len : %v", err)
	}
	if n != len(MarquesLyra) {
		t.Errorf("Len = %d, veut %d — une marque est invisible de l'adaptateur", n, len(MarquesLyra))
	}

	all, err := s.AllTransactions()
	if err != nil {
		t.Fatalf("AllTransactions : %v", err)
	}
	if len(all) != len(MarquesLyra) {
		t.Fatalf("AllTransactions = %d, veut %d", len(all), len(MarquesLyra))
	}

	for _, marque := range MarquesLyra {
		tx, err := s.ByToken("tok-" + marque)
		if err != nil {
			t.Fatalf("ByToken %s : %v", marque, err)
		}
		if tx == nil {
			t.Errorf("ByToken %s = nil : le formToken d'une marque n'est pas retrouvé", marque)
			continue
		}
		if tx.Brand != marque {
			t.Errorf("ByToken %s : Brand = %q, la marque ne survit pas à l'aller-retour", marque, tx.Brand)
		}
		byUUID, err := s.ByUUID("u-" + marque)
		if err != nil || byUUID == nil {
			t.Errorf("ByUUID %s : %v / %v", marque, byUUID, err)
		}
	}
}

// TestRepoStore_marqueVideVautDefaut : un appelant qui n'exprime pas de
// marque doit produire un enregistrement visible, pas un provider vide
// que le filtre écarterait.
func TestRepoStore_marqueVideVautDefaut(t *testing.T) {
	t.Parallel()
	s := newMemStore()
	p, err := domain.New(clock.System{}, "u-vide", 1000, "EUR")
	if err != nil {
		t.Fatalf("domain.New : %v", err)
	}
	if err := s.Save(&Transaction{
		FormToken: "tok-vide", UUID: "u-vide", OrderID: "CMD", Amount: 1000,
		Currency: "EUR", Payment: p,
	}); err != nil {
		t.Fatalf("Save : %v", err)
	}
	tx, err := s.ByUUID("u-vide")
	if err != nil || tx == nil {
		t.Fatalf("ByUUID = %v, %v", tx, err)
	}
	if tx.Brand != MarqueParDefaut {
		t.Errorf("Brand = %q, veut %q", tx.Brand, MarqueParDefaut)
	}
}

func TestEstMarqueLyra(t *testing.T) {
	t.Parallel()
	for _, m := range MarquesLyra {
		if !EstMarqueLyra(m) {
			t.Errorf("%q rejetee alors qu'elle est dans la liste", m)
		}
	}
	for _, m := range []string{"stripe", "monetico", "", "PAYZEN"} {
		if EstMarqueLyra(m) {
			t.Errorf("%q acceptee a tort", m)
		}
	}
}
