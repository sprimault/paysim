// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/domain"
	sqlitepkg "github.com/sprimault/paysim/internal/store/sqlite"
)

// openTestStore construit un RepoStore end-to-end : ouvre la base
// SQLite, prépare les trois repositories (payments, subscriptions,
// payment methods), wrappe dans un RepoStore. Cleanup automatique.
func openTestStore(t *testing.T, path string) *RepoStore {
	t.Helper()
	db, err := sqlitepkg.Open(path)
	if err != nil {
		t.Fatalf("sqlite Open: %v", err)
	}
	repo, err := sqlitepkg.NewPaymentsRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewPaymentsRepository: %v", err)
	}
	subsRepo, err := sqlitepkg.NewSubscriptionsRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewSubscriptionsRepository: %v", err)
	}
	methodsRepo, err := sqlitepkg.NewPaymentMethodsRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewPaymentMethodsRepository: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewRepoStore(repo, subsRepo, methodsRepo)
}

// runContract lance le même scénario sur une Store — vérifie que
// Adosse a inmem ou a SQLite, RepoStore respecte le contrat au bit
// près (même comportement pour un même input).
func runContract(t *testing.T, s Store) {
	t.Helper()
	tx := buildSampleTx(t)

	if err := s.Save(tx); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.ByToken(tx.FormToken)
	if err != nil {
		t.Fatalf("ByToken: %v", err)
	}
	if got == nil {
		t.Fatal("ByToken = nil, veut la transaction")
	}
	if got.OrderID != tx.OrderID {
		t.Errorf("OrderID = %q, veut %q", got.OrderID, tx.OrderID)
	}
	if got.Payment.State() != tx.Payment.State() {
		t.Errorf("State = %q, veut %q", got.Payment.State(), tx.Payment.State())
	}
	if len(got.Payment.Events()) != len(tx.Payment.Events()) {
		t.Errorf("events = %d, veut %d", len(got.Payment.Events()), len(tx.Payment.Events()))
	}
	if got.ReturnURL != tx.ReturnURL {
		t.Errorf("ReturnURL = %q, veut %q", got.ReturnURL, tx.ReturnURL)
	}
	if got.NotificationURL != tx.NotificationURL {
		t.Errorf("NotificationURL = %q, veut %q", got.NotificationURL, tx.NotificationURL)
	}
	if got.Customer != tx.Customer {
		t.Errorf("Customer = %+v, veut %+v", got.Customer, tx.Customer)
	}

	byUUID, _ := s.ByUUID(tx.UUID)
	if byUUID == nil || byUUID.UUID != tx.UUID {
		t.Errorf("ByUUID = %+v", byUUID)
	}

	all, err := s.AllTransactions()
	if err != nil {
		t.Fatalf("AllTransactions: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("AllTransactions = %d, veut 1", len(all))
	}

	n, _ := s.Len()
	if n != 1 {
		t.Errorf("Len = %d, veut 1", n)
	}

	// Cles vides — ajoute apres coup, et pas par gout de l'exhaustivite.
	//
	// L'implementation memoire les acceptait la ou SQLite les refusait :
	// une transaction sans UUID s'y enregistrait, et une recherche a vide
	// la retrouvait. Deux backends censes etre interchangeables ne
	// l'etaient donc pas, et ce contrat ne le voyait pas parce qu'il
	// n'exercait que des transactions completes.
	// La transaction porte un Payment valide et un UUID vide : sans lui,
	// Save echouerait des la traduction, sur le Payment nil, et ce test
	// passerait meme sans la garde qu'il pretend verifier.
	sansUUID := buildSampleTx(t)
	sansUUID.UUID = ""
	sansUUID.FormToken = "sans-uuid"
	if err := s.Save(sansUUID); err == nil {
		t.Error("Save d'une transaction sans UUID = nil, veut une erreur")
	}
	if got, err := s.ByUUID(""); err != nil || got != nil {
		t.Errorf("ByUUID(\"\") = %+v, %v — veut nil, nil : une cle vide ne doit rien trouver",
			got, err)
	}
	if got, err := s.ByToken(""); err != nil || got != nil {
		t.Errorf("ByToken(\"\") = %+v, %v — veut nil, nil", got, err)
	}
	if n, _ := s.Len(); n != 1 {
		t.Errorf("Len = %d apres les appels a vide, veut 1 — rien n'a du etre ajoute", n)
	}
}

// buildSampleTx : Transaction test avec Payment domain non trivial
// (créé + capturé = 2 events, state=captured).
func buildSampleTx(t *testing.T) *Transaction {
	t.Helper()
	p, err := domain.New("uuid-1", 4990, "EUR")
	if err != nil {
		t.Fatalf("domain.New: %v", err)
	}
	if err := p.Capture(); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	now := time.Now().UTC()
	return &Transaction{
		FormToken:       "form-token-42",
		UUID:            "uuid-1",
		OrderID:         "CMD-42",
		Amount:          4990,
		Currency:        "EUR",
		FormAction:      "PAYMENT",
		Customer:        Customer{Email: "cli@example.com"},
		Metadata:        map[string]string{"k": "v"},
		Payment:         p,
		ReturnURL:       "https://m.example/back",
		NotificationURL: "https://m.example/ipn",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func TestRepoStoreContract(t *testing.T) {
	t.Parallel()
	s := openTestStore(t, filepath.Join(t.TempDir(), "test.db"))
	runContract(t, s)
}

func TestRepoStoreMemoireContract(t *testing.T) {
	t.Parallel()
	runContract(t, newMemStore())
}

// runMethodContract vérifie les 3 méthodes de PaymentMethod du contrat
// Store. Passé sur les deux backends de RepoStore pour garantir la parité.
func runMethodContract(t *testing.T, s Store) {
	t.Helper()
	m := &PaymentMethod{
		Token:       "pmt-test",
		PANFull:     "4111111111111111",
		PANMasked:   "411111XXXXXX1111",
		Brand:       "VISA",
		ExpiryMonth: 12,
		ExpiryYear:  2027,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.SaveMethod(m); err != nil {
		t.Fatalf("SaveMethod: %v", err)
	}
	got, err := s.MethodByToken("pmt-test")
	if err != nil {
		t.Fatalf("MethodByToken: %v", err)
	}
	if got == nil {
		t.Fatal("MethodByToken = nil, veut le PaymentMethod")
	}
	if got.Brand != "VISA" || got.ExpiryYear != 2027 {
		t.Errorf("Brand/ExpiryYear = %q/%d", got.Brand, got.ExpiryYear)
	}
	if got.Revoked {
		t.Errorf("Revoked = true, veut false a l'init")
	}

	if err := s.RevokeMethod("pmt-test"); err != nil {
		t.Fatalf("RevokeMethod: %v", err)
	}
	got2, _ := s.MethodByToken("pmt-test")
	if got2 == nil || !got2.Revoked {
		t.Errorf("apres RevokeMethod, Revoked = %+v, veut true", got2)
	}

	// Idempotence : revoke sur token inconnu ne remonte pas d'erreur.
	if err := s.RevokeMethod("inconnu"); err != nil {
		t.Errorf("RevokeMethod(inconnu) = %v, veut nil", err)
	}
	// ByToken sur inconnu retourne nil sans erreur.
	if got3, err := s.MethodByToken("inconnu"); err != nil || got3 != nil {
		t.Errorf("MethodByToken(inconnu) = %+v, %v ; veut nil, nil", got3, err)
	}
}

// runSubscriptionContract vérifie les 3 méthodes de Subscription du
// contrat Store. Passé sur les deux backends de RepoStore.
func runSubscriptionContract(t *testing.T, s Store) {
	t.Helper()
	sub := &Subscription{
		ID:                 "sub-test",
		OrderID:            "SUB-42",
		Amount:             2990,
		Currency:           "EUR",
		PaymentMethodToken: "pmt-x",
		EffectDate:         "2026-09-01T00:00:00Z",
		Rrule:              "RRULE:FREQ=MONTHLY;INTERVAL=1",
		Metadata:           map[string]string{"plan": "pro"},
		CreatedAt:          time.Now().UTC(),
	}
	if err := s.SaveSubscription(sub); err != nil {
		t.Fatalf("SaveSubscription: %v", err)
	}
	got, err := s.SubscriptionByID("sub-test")
	if err != nil {
		t.Fatalf("SubscriptionByID: %v", err)
	}
	if got == nil {
		t.Fatal("SubscriptionByID = nil, veut la subscription")
	}
	if got.PaymentMethodToken != "pmt-x" || got.Rrule != "RRULE:FREQ=MONTHLY;INTERVAL=1" {
		t.Errorf("token/rrule = %q/%q", got.PaymentMethodToken, got.Rrule)
	}
	n, _ := s.LenSubscriptions()
	if n != 1 {
		t.Errorf("LenSubscriptions = %d, veut 1", n)
	}
	if got.Metadata["plan"] != "pro" {
		t.Errorf("Metadata[plan] = %q, veut pro (round-trip)", got.Metadata["plan"])
	}
	// Régression : le champ Cancelled doit être persisté et relu.
	// Un oubli d'aller-retour ici casse silencieusement CancelSubscription
	// en mode SQLite — les tests API ne voient rien parce qu'ils tournent
	// en mémoire, et les scénarios canoniques échouent seulement à
	// l'exécution contre une instance persistée.
	if got.Cancelled {
		t.Fatalf("Cancelled = true à la création, veut false")
	}
	sub.Cancelled = true
	if err := s.SaveSubscription(sub); err != nil {
		t.Fatalf("SaveSubscription(cancelled=true): %v", err)
	}
	got, err = s.SubscriptionByID("sub-test")
	if err != nil {
		t.Fatalf("SubscriptionByID après cancel: %v", err)
	}
	if !got.Cancelled {
		t.Errorf("Cancelled = false après SaveSubscription(cancelled=true), veut true")
	}
}

func TestRepoStoreMethodContract(t *testing.T) {
	t.Parallel()
	runMethodContract(t, openTestStore(t, filepath.Join(t.TempDir(), "methods.db")))
}

func TestRepoStoreMemoireMethodContract(t *testing.T) {
	t.Parallel()
	runMethodContract(t, newMemStore())
}

func TestRepoStoreSubscriptionContract(t *testing.T) {
	t.Parallel()
	runSubscriptionContract(t, openTestStore(t, filepath.Join(t.TempDir(), "subs.db")))
}

func TestRepoStoreMemoireSubscriptionContract(t *testing.T) {
	t.Parallel()
	runSubscriptionContract(t, newMemStore())
}

func TestRepoStoreSurvivesReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "persist.db")

	// Écriture initiale — le db est fermé après cette portée.
	func() {
		db, err := sqlitepkg.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		repo, err := sqlitepkg.NewPaymentsRepository(db)
		if err != nil {
			t.Fatal(err)
		}
		subsRepo, err := sqlitepkg.NewSubscriptionsRepository(db)
		if err != nil {
			t.Fatal(err)
		}
		methodsRepo, err := sqlitepkg.NewPaymentMethodsRepository(db)
		if err != nil {
			t.Fatal(err)
		}
		s1 := NewRepoStore(repo, subsRepo, methodsRepo)
		if err := s1.Save(buildSampleTx(t)); err != nil {
			t.Fatal(err)
		}
	}()

	// Ré-ouverture : les données sont là.
	s2 := openTestStore(t, path)
	tx, err := s2.ByToken("form-token-42")
	if err != nil {
		t.Fatal(err)
	}
	if tx == nil {
		t.Fatal("ByToken = nil apres reopen — persistance cassee")
	}
	if tx.Payment.State() != domain.StateCaptured {
		t.Errorf("State = %q apres reopen", tx.Payment.State())
	}
	if len(tx.Payment.Events()) != 2 {
		t.Errorf("events = %d apres reopen", len(tx.Payment.Events()))
	}
}

// Le client de l'alias doit survivre à l'aller-retour SQLite : sans lui
// en base, le rejeu perdrait son autorité au premier redémarrage.
func TestSQLiteMethodPersisteLeClient(t *testing.T) {
	t.Parallel()
	chemin := filepath.Join(t.TempDir(), "pm.db")
	s := openTestStore(t, chemin)

	pm := NewPaymentMethod("tok-cli", Card{
		PAN: "5555555555554444", ExpiryMonth: 12, ExpiryYear: 2030,
	}, Customer{
		Reference: "client-A", Email: "a@example.com",
		BillingDetails: BillingDetails{LastName: "MARTIN", Country: "FR"},
	}, time.Now().UTC())
	if err := s.SaveMethod(pm); err != nil {
		t.Fatal(err)
	}

	relu, err := s.MethodByToken("tok-cli")
	if err != nil || relu == nil {
		t.Fatalf("relecture : %v", err)
	}
	if relu.Customer.Reference != "client-A" || relu.Customer.Email != "a@example.com" {
		t.Errorf("client relu = %+v", relu.Customer)
	}
	if relu.Customer.BillingDetails.LastName != "MARTIN" ||
		relu.Customer.BillingDetails.Country != "FR" {
		t.Errorf("billingDetails relus = %+v", relu.Customer.BillingDetails)
	}
}
