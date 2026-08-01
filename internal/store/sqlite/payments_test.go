// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/store"
)

// buildRepo ouvre un repository dans un tempdir isolé — chaque test
// démarre avec une base vierge.
func buildRepo(t *testing.T) *PaymentsRepository {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	repo, err := NewPaymentsRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewPaymentsRepository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

// sampleRecord construit un PaymentRecord de test avec des valeurs
// couvrant le core normalisé (colonnes typées) et les blobs JSON.
func sampleRecord(uuid, ref string) *store.PaymentRecord {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	return &store.PaymentRecord{
		UUID:             uuid,
		Provider:         "payzen",
		ProviderRef:      ref,
		OrderID:          "CMD-" + uuid,
		Amount:           4990,
		Currency:         "EUR",
		State:            domain.StateCaptured,
		Refunded:         0,
		CustomerJSON:     `{"email":"cli@example.com"}`,
		MetadataJSON:     `{"src":"test"}`,
		ProviderDataJSON: `{"returnUrl":"https://m.example/back"}`,
		Events: []domain.Event{
			{At: now, Kind: domain.EventCreated, Amount: 4990},
			{At: now.Add(time.Second), Kind: domain.EventCaptured, Amount: 4990},
		},
		CreatedAt: now,
		UpdatedAt: now.Add(2 * time.Second),
	}
}

func TestSaveAndByUUID(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	rec := sampleRecord("u1", "form-token-1")

	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.ByUUID("u1")
	if err != nil {
		t.Fatalf("ByUUID: %v", err)
	}
	if got == nil {
		t.Fatal("ByUUID = nil, veut le record")
	}
	if got.Provider != "payzen" || got.ProviderRef != "form-token-1" {
		t.Errorf("got.Provider/Ref = %q/%q", got.Provider, got.ProviderRef)
	}
	if got.State != domain.StateCaptured {
		t.Errorf("State = %q", got.State)
	}
	if len(got.Events) != 2 {
		t.Errorf("events = %d, veut 2", len(got.Events))
	}
	if got.CustomerJSON != rec.CustomerJSON {
		t.Errorf("CustomerJSON = %q", got.CustomerJSON)
	}
}

func TestByProviderRefLookup(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	_ = repo.Save(sampleRecord("u1", "form-A"))
	_ = repo.Save(sampleRecord("u2", "form-B"))

	got, err := repo.ByProviderRef("payzen", "form-B")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.UUID != "u2" {
		t.Errorf("got = %+v, veut UUID=u2", got)
	}
	// Provider différent → nil.
	got, err = repo.ByProviderRef("stripe", "form-B")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("stripe/form-B = %+v, veut nil", got)
	}
}

func TestSaveIsUpsert(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	rec := sampleRecord("u1", "form-1")
	_ = repo.Save(rec)

	// Nouvelle version : state change, un nouvel event.
	rec.State = domain.StateRefunded
	rec.Refunded = 4990
	rec.Events = append(rec.Events, domain.Event{
		At: time.Now().UTC(), Kind: domain.EventRefunded, Amount: 4990,
	})
	if err := repo.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.ByUUID("u1")
	if got.State != domain.StateRefunded {
		t.Errorf("State = %q, veut refunded", got.State)
	}
	if got.Refunded != 4990 {
		t.Errorf("Refunded = %d", got.Refunded)
	}
	if len(got.Events) != 3 {
		t.Errorf("events = %d, veut 3", len(got.Events))
	}
	// Count reste 1 (upsert, pas insert).
	n, _ := repo.Count()
	if n != 1 {
		t.Errorf("Count = %d, veut 1", n)
	}
}

func TestAllOrderedByUpdatedAtDesc(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	oldRec := sampleRecord("old", "ref-old")
	oldRec.UpdatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newRec := sampleRecord("new", "ref-new")
	newRec.UpdatedAt = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	_ = repo.Save(oldRec)
	_ = repo.Save(newRec)

	all, err := repo.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("All len = %d, veut 2", len(all))
	}
	if all[0].UUID != "new" || all[1].UUID != "old" {
		t.Errorf("ordre = %q,%q, veut new,old", all[0].UUID, all[1].UUID)
	}
}

func TestByProviderFilters(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	rP := sampleRecord("p1", "form-1")
	rP.Provider = "payzen"
	rS := sampleRecord("s1", "pi_xxx")
	rS.Provider = "stripe"
	_ = repo.Save(rP)
	_ = repo.Save(rS)

	pz, _ := repo.ByProvider("payzen")
	if len(pz) != 1 || pz[0].Provider != "payzen" {
		t.Errorf("payzen filter = %+v", pz)
	}
	st, _ := repo.ByProvider("stripe")
	if len(st) != 1 || st[0].Provider != "stripe" {
		t.Errorf("stripe filter = %+v", st)
	}
	all, _ := repo.All()
	if len(all) != 2 {
		t.Errorf("All = %d, veut 2", len(all))
	}
}

func TestUnknownReturnsNil(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	got, _ := repo.ByUUID("inexistant")
	if got != nil {
		t.Errorf("ByUUID inexistant = %+v", got)
	}
	got, _ = repo.ByProviderRef("payzen", "inexistant")
	if got != nil {
		t.Errorf("ByProviderRef inexistant = %+v", got)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "persist.db")

	// Session 1 : écrire, fermer.
	db1, _ := Open(path)
	repo1, _ := NewPaymentsRepository(db1)
	_ = repo1.Save(sampleRecord("u1", "ref-1"))
	_ = repo1.Close()

	// Session 2 : rouvrir, lire.
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	repo2, err := NewPaymentsRepository(db2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo2.Close() }()

	got, _ := repo2.ByUUID("u1")
	if got == nil {
		t.Fatal("ByUUID = nil apres reopen — persistance cassee")
	}
	if len(got.Events) != 2 {
		t.Errorf("events reloaded = %d, veut 2", len(got.Events))
	}
}
