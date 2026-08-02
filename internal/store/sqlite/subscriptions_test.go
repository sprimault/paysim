// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

func buildSubsRepo(t *testing.T) *SubscriptionsRepository {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "subs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	repo, err := NewSubscriptionsRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewSubscriptionsRepository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close(); _ = db.Close() })
	return repo
}

func sampleSubscription(id, provider, token string) *store.SubscriptionRecord {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	return &store.SubscriptionRecord{
		ID:                 id,
		Provider:           provider,
		OrderID:            "SUB-" + id,
		Amount:             2990,
		Currency:           "EUR",
		PaymentMethodToken: token,
		EffectDate:         "2026-09-01T00:00:00Z",
		Rrule:              "RRULE:FREQ=MONTHLY;INTERVAL=1",
		MetadataJSON:       `{"plan":"pro"}`,
		ProviderDataJSON:   `{}`,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func TestSubscriptions_saveAndByID(t *testing.T) {
	t.Parallel()
	repo := buildSubsRepo(t)
	if err := repo.Save(sampleSubscription("sub-1", "payzen", "pmt-1")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.ByID("sub-1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got == nil {
		t.Fatal("ByID = nil, veut le record")
	}
	if got.PaymentMethodToken != "pmt-1" {
		t.Errorf("PaymentMethodToken = %q", got.PaymentMethodToken)
	}
	if got.Amount != 2990 || got.Currency != "EUR" {
		t.Errorf("Amount/Currency = %d/%q", got.Amount, got.Currency)
	}
	if got.Rrule != "RRULE:FREQ=MONTHLY;INTERVAL=1" {
		t.Errorf("Rrule = %q", got.Rrule)
	}
}

func TestSubscriptions_byIDInconnu(t *testing.T) {
	t.Parallel()
	repo := buildSubsRepo(t)
	got, err := repo.ByID("does-not-exist")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got != nil {
		t.Errorf("ByID(inconnu) = %+v, veut nil", got)
	}
}

func TestSubscriptions_saveEcrase(t *testing.T) {
	t.Parallel()
	repo := buildSubsRepo(t)
	rec := sampleSubscription("sub-1", "payzen", "pmt-1")
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save initial: %v", err)
	}
	rec.OrderID = "SUB-1-updated"
	rec.UpdatedAt = rec.UpdatedAt.Add(time.Hour)
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save maj: %v", err)
	}
	got, _ := repo.ByID("sub-1")
	if got.OrderID != "SUB-1-updated" {
		t.Errorf("OrderID = %q, veut SUB-1-updated", got.OrderID)
	}
}

func TestSubscriptions_byProviderTri(t *testing.T) {
	t.Parallel()
	repo := buildSubsRepo(t)
	// Trois subs pour payzen, une pour stripe. Ordre updated_at DESC.
	rec1 := sampleSubscription("sub-1", "payzen", "p1")
	rec1.UpdatedAt = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rec2 := sampleSubscription("sub-2", "payzen", "p2")
	rec2.UpdatedAt = time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	rec3 := sampleSubscription("sub-3", "payzen", "p3")
	rec3.UpdatedAt = time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	recStripe := sampleSubscription("sub-4", "stripe", "s1")

	for _, r := range []*store.SubscriptionRecord{rec1, rec2, rec3, recStripe} {
		if err := repo.Save(r); err != nil {
			t.Fatalf("Save %s: %v", r.ID, err)
		}
	}

	got, err := repo.ByProvider("payzen")
	if err != nil {
		t.Fatalf("ByProvider: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("payzen count = %d, veut 3", len(got))
	}
	wantOrder := []string{"sub-2", "sub-3", "sub-1"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("got[%d].ID = %q, veut %q", i, got[i].ID, want)
		}
	}

	stripe, _ := repo.ByProvider("stripe")
	if len(stripe) != 1 || stripe[0].ID != "sub-4" {
		t.Errorf("stripe = %+v, veut 1 sub-4", stripe)
	}
}

func TestSubscriptions_count(t *testing.T) {
	t.Parallel()
	repo := buildSubsRepo(t)
	if n, _ := repo.Count(); n != 0 {
		t.Errorf("Count initial = %d, veut 0", n)
	}
	_ = repo.Save(sampleSubscription("s1", "payzen", "p"))
	_ = repo.Save(sampleSubscription("s2", "stripe", "p"))
	if n, _ := repo.Count(); n != 2 {
		t.Errorf("Count = %d, veut 2", n)
	}
}

func TestSubscriptions_deleteByID(t *testing.T) {
	t.Parallel()
	repo := buildSubsRepo(t)
	_ = repo.Save(sampleSubscription("s1", "payzen", "p"))
	if err := repo.DeleteByID("s1"); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
	if got, _ := repo.ByID("s1"); got != nil {
		t.Errorf("apres delete, ByID = %+v, veut nil", got)
	}
	// Idempotent : redelete ne casse pas.
	if err := repo.DeleteByID("s1"); err != nil {
		t.Errorf("DeleteByID sur inconnu = %v, veut nil", err)
	}
}

func TestSubscriptions_deleteByProvider(t *testing.T) {
	t.Parallel()
	repo := buildSubsRepo(t)
	_ = repo.Save(sampleSubscription("s1", "payzen", "p"))
	_ = repo.Save(sampleSubscription("s2", "payzen", "p"))
	_ = repo.Save(sampleSubscription("s3", "stripe", "p"))
	n, err := repo.DeleteByProvider("payzen")
	if err != nil {
		t.Fatalf("DeleteByProvider: %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d, veut 2", n)
	}
	if c, _ := repo.Count(); c != 1 {
		t.Errorf("apres delete, Count = %d, veut 1 (stripe reste)", c)
	}
}
