// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"testing"
)

func TestDeleteByUUIDRemovesRecordAndEvents(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	rec := sampleRecord("u1", "form-1")
	if err := repo.Save(rec); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteByUUID("u1"); err != nil {
		t.Fatalf("DeleteByUUID: %v", err)
	}
	got, _ := repo.ByUUID("u1")
	if got != nil {
		t.Errorf("ByUUID = %+v, veut nil apres delete", got)
	}
	// Vérifier que les events ont bien été supprimés par CASCADE.
	events, err := repo.loadEvents("u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("events restants = %d, veut 0 (CASCADE)", len(events))
	}
}

func TestDeleteByUUIDIdempotent(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	// Delete d'un UUID inexistant : pas d'erreur.
	if err := repo.DeleteByUUID("inconnu"); err != nil {
		t.Errorf("DeleteByUUID inconnu = %v, veut nil", err)
	}
	if err := repo.DeleteByUUID(""); err != nil {
		t.Errorf("DeleteByUUID vide = %v, veut nil", err)
	}
}

func TestDeleteByProviderCountsAndFilters(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	pz := sampleRecord("pz1", "form-pz")
	pz.Provider = "payzen"
	st := sampleRecord("st1", "pi_stripe")
	st.Provider = "stripe"
	_ = repo.Save(pz)
	_ = repo.Save(st)

	n, err := repo.DeleteByProvider("payzen")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("deleted payzen = %d, veut 1", n)
	}
	// Stripe reste.
	got, _ := repo.ByUUID("st1")
	if got == nil {
		t.Error("stripe ne doit pas être supprimé par DeleteByProvider(payzen)")
	}
	got, _ = repo.ByUUID("pz1")
	if got != nil {
		t.Errorf("payzen doit être supprimé, got %+v", got)
	}
}

func TestDeleteAllPurgesEverything(t *testing.T) {
	t.Parallel()
	repo := buildRepo(t)
	_ = repo.Save(sampleRecord("u1", "form-1"))
	_ = repo.Save(sampleRecord("u2", "form-2"))
	pz3 := sampleRecord("u3", "pi_3")
	pz3.Provider = "stripe"
	_ = repo.Save(pz3)

	n, err := repo.DeleteAll()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("deleted total = %d, veut 3", n)
	}
	count, _ := repo.Count()
	if count != 0 {
		t.Errorf("Count = %d apres DeleteAll, veut 0", count)
	}
}
