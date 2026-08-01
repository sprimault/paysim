// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/domain"
)

// runContract lance le même scénario sur une Store — vérifie que
// MemoryStore et SQLiteStore respectent tous deux le contrat au bit
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

func TestSQLiteStoreContract(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	runContract(t, s)
}

func TestMemoryStoreContract(t *testing.T) {
	t.Parallel()
	runContract(t, NewMemoryStore())
}

func TestSQLiteStoreSurvivesReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "persist.db")

	// Écriture initiale.
	s1, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Save(buildSampleTx(t)); err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()

	// Ré-ouverture : les données sont là.
	s2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()

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
