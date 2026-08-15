// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

// buildWebhookRepo ouvre un repo dans un tempdir isolé.
func buildWebhookRepo(t *testing.T) *WebhooksRepository {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	repo, err := NewWebhooksRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewWebhooksRepository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func sampleWebhook(id string, completedAt time.Time) *store.WebhookRecord {
	return &store.WebhookRecord{
		ID:          id,
		URL:         "https://merchant/callback",
		HeadersJSON: `{"Content-Type":"application/x-www-form-urlencoded"}`,
		Body:        []byte("kr-hash=abc&kr-answer=%7B%22orderStatus%22%3A%22PAID%22%7D"),
		Status:      "delivered",
		Outcome:     "PAID",
		StatusCode:  200,
		ErrorMsg:    "",
		Attempts:    1,
		CreatedAt:   completedAt.Add(-100 * time.Millisecond),
		CompletedAt: completedAt,
	}
}

func TestSaveAndByIDWebhook(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	now := time.Now().UTC()
	rec := sampleWebhook("wh-1", now)

	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.ByID("wh-1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got == nil {
		t.Fatal("ByID = nil, veut le record")
	}
	if got.URL != rec.URL || got.Status != rec.Status || got.StatusCode != rec.StatusCode {
		t.Errorf("mismatch : %+v", got)
	}
	if string(got.Body) != string(rec.Body) {
		t.Errorf("body mismatch")
	}
}

func TestRecentWebhooksOrderedDesc(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	_ = repo.Save(sampleWebhook("wh-old", base))
	_ = repo.Save(sampleWebhook("wh-mid", base.Add(time.Second)))
	_ = repo.Save(sampleWebhook("wh-new", base.Add(2*time.Second)))

	got, err := repo.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, veut 3", len(got))
	}
	if got[0].ID != "wh-new" || got[2].ID != "wh-old" {
		t.Errorf("ordre = %s,%s,%s", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestRecentWebhooksLimit(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_ = repo.Save(sampleWebhook("wh-"+string(rune('a'+i)),
			base.Add(time.Duration(i)*time.Second)))
	}
	got, _ := repo.Recent(3)
	if len(got) != 3 {
		t.Errorf("len = %d, veut 3", len(got))
	}
}

func TestByIDUnknownReturnsNil(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	got, err := repo.ByID("inconnu")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("ByID inconnu = %+v", got)
	}
}

func TestSaveIsUpsertWebhook(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	now := time.Now().UTC()
	rec := sampleWebhook("wh-1", now)
	_ = repo.Save(rec)

	// Deuxième tentative (rejeu ratée) — modifier state.
	rec.Status = "failed"
	rec.StatusCode = 500
	rec.ErrorMsg = "server error"
	rec.Attempts = 2
	if err := repo.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.ByID("wh-1")
	if got.Status != "failed" || got.Attempts != 2 || got.ErrorMsg != "server error" {
		t.Errorf("upsert non appliqué : %+v", got)
	}
}

func TestDeleteAllWebhooks(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	base := time.Now().UTC()
	_ = repo.Save(sampleWebhook("wh-1", base))
	_ = repo.Save(sampleWebhook("wh-2", base))
	_ = repo.Save(sampleWebhook("wh-3", base))

	n, err := repo.DeleteAll()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("deleted = %d, veut 3", n)
	}
	got, _ := repo.Recent(10)
	if len(got) != 0 {
		t.Errorf("apres purge : len = %d", len(got))
	}
}

// TestWebhooks_outcomeRoundTrip verifie que le resultat metier annonce
// par le webhook survit a l'aller-retour SQLite. Sans lui, la colonne
// pourrait etre ecrite sans jamais etre relue — les assertions de
// scenario compteraient alors zero webhook.
func TestWebhooks_outcomeRoundTrip(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	now := time.Now().UTC()

	rec := sampleWebhook("wh-outcome", now)
	rec.Outcome = "UNPAID"
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.ByID("wh-outcome")
	if err != nil || got == nil {
		t.Fatalf("ByID: %v / %v", got, err)
	}
	if got.Outcome != "UNPAID" {
		t.Errorf("Outcome = %q, veut UNPAID", got.Outcome)
	}
	// Status et Outcome doivent rester independants : un webhook remis
	// avec succes peut annoncer un refus.
	if got.Status != "delivered" {
		t.Errorf("Status = %q, veut delivered", got.Status)
	}
}

// TestWebhooks_byPayment est le test que l'absence de rattachement
// rendait impossible : avant la colonne payment_uuid, rien ne
// distinguait les livraisons d'un paiement de celles d'un autre, et
// l'UI affichait le kr-answer du dernier webhook de l'instance.
func TestWebhooks_byPayment(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		id      string
		payment string
		at      time.Time
	}{
		{"wh-a1", "pay-a", base},
		{"wh-b1", "pay-b", base.Add(time.Second)},
		{"wh-a2", "pay-a", base.Add(2 * time.Second)},
		{"wh-orphan", "", base.Add(3 * time.Second)},
	} {
		rec := sampleWebhook(tc.id, tc.at)
		rec.PaymentUUID = tc.payment
		if err := repo.Save(rec); err != nil {
			t.Fatalf("Save %s: %v", tc.id, err)
		}
	}

	got, err := repo.ByPayment("pay-a", 10)
	if err != nil {
		t.Fatalf("ByPayment: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, veut 2 — les livraisons d'un autre paiement ont fuite", len(got))
	}
	// Plus recente d'abord : c'est celle-la que l'onglet Payload affiche.
	if got[0].ID != "wh-a2" || got[1].ID != "wh-a1" {
		t.Errorf("ordre = %s,%s, veut wh-a2,wh-a1", got[0].ID, got[1].ID)
	}

	// Un uuid vide ne doit pas ramener les orphelins : les livraisons
	// sans paiement rattache ne forment pas un ensemble consultable.
	empty, err := repo.ByPayment("", 10)
	if err != nil {
		t.Fatalf("ByPayment(vide): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("ByPayment(vide) = %d entrees, veut 0", len(empty))
	}

	// Un paiement sans livraison ne remonte rien, plutot que tout.
	none, _ := repo.ByPayment("pay-inconnu", 10)
	if len(none) != 0 {
		t.Errorf("paiement inconnu = %d entrees, veut 0", len(none))
	}
}

func TestWebhooks_paymentUUIDRoundTrip(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)

	rec := sampleWebhook("wh-uuid", time.Now().UTC())
	rec.PaymentUUID = "7917f17b-db5e-4b21-9d20-295a3f0c422b"
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.ByID("wh-uuid")
	if err != nil || got == nil {
		t.Fatalf("ByID: %v / %v", got, err)
	}
	if got.PaymentUUID != rec.PaymentUUID {
		t.Errorf("PaymentUUID = %q, veut %q", got.PaymentUUID, rec.PaymentUUID)
	}
}

// TestWebhooks_migrateExistingTable exerce le chemin ALTER TABLE sur une
// base creee avant l'ajout des colonnes outcome et payment_uuid.
func TestWebhooks_migrateExistingTable(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "legacy-wh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const legacy = `CREATE TABLE webhooks (
		id TEXT PRIMARY KEY,
		url TEXT NOT NULL,
		headers_json TEXT NOT NULL DEFAULT '{}',
		body BLOB NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		status_code INTEGER NOT NULL DEFAULT 0,
		error_msg TEXT NOT NULL DEFAULT '',
		attempts INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		completed_at TEXT NOT NULL
	)`
	if _, err := db.ExecContext(t.Context(), legacy); err != nil {
		t.Fatalf("schema ancien: %v", err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO webhooks
		(id, url, headers_json, body, status, status_code, error_msg, attempts, created_at, completed_at)
		VALUES ('wh-old', 'https://m/cb', '{}', '', 'delivered', 200, '', 1,
		        '2026-08-02T10:00:00Z', '2026-08-02T10:00:01Z')`); err != nil {
		t.Fatalf("ligne ancienne: %v", err)
	}

	repo, err := NewWebhooksRepository(db)
	if err != nil {
		t.Fatalf("migration sur base existante: %v", err)
	}
	got, err := repo.ByID("wh-old")
	if err != nil || got == nil {
		t.Fatalf("la ligne preexistante a disparu: %v / %v", got, err)
	}
	if got.Status != "delivered" {
		t.Errorf("Status altere: %q", got.Status)
	}
	// Une livraison historisee avant la migration n'a pas d'outcome : on
	// ne peut pas le reconstituer sans relire son corps.
	if got.Outcome != "" {
		t.Errorf("Outcome = %q, veut vide sur une ligne ancienne", got.Outcome)
	}
}

// Le découpage en lots est du code à nous, pas au moteur : au-delà de
// lotCascade, l'instruction est scindée et les comptes s'additionnent.
// Un test à trois UUID ne l'exercerait jamais.
func TestDeleteByPayment_lots(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	const vises = 600
	uuids := make([]string, 0, vises)
	for i := 0; i < vises; i++ {
		uuid := fmt.Sprintf("pay-%04d", i)
		uuids = append(uuids, uuid)
		rec := sampleWebhook(fmt.Sprintf("wh-%04d", i), base.Add(time.Duration(i)*time.Second))
		rec.PaymentUUID = uuid
		if err := repo.Save(rec); err != nil {
			t.Fatalf("Save %s: %v", uuid, err)
		}
	}
	// Un témoin qu'aucun lot ne doit emporter.
	temoin := sampleWebhook("wh-temoin", base.Add(time.Hour))
	temoin.PaymentUUID = "pay-temoin"
	if err := repo.Save(temoin); err != nil {
		t.Fatalf("Save temoin: %v", err)
	}

	n, err := repo.DeleteByPayment(uuids...)
	if err != nil {
		t.Fatalf("DeleteByPayment: %v", err)
	}
	if n != vises {
		t.Errorf("supprimes = %d, veut %d", n, vises)
	}
	// Relire plutôt que se fier au compte : RowsAffected ne dit rien
	// d'un prédicat mal écrit qui aurait aussi emporté le témoin.
	restants, err := repo.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(restants) != 1 || restants[0].ID != "wh-temoin" {
		t.Errorf("restants = %+v, veut le seul temoin", restants)
	}
}

// Les doublons ne doivent ni gonfler l'instruction ni fausser le compte.
func TestDeleteByPayment_doublonsEtVides(t *testing.T) {
	t.Parallel()
	repo := buildWebhookRepo(t)
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	rec := sampleWebhook("wh-1", base)
	rec.PaymentUUID = "pay-a"
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	orpheline := sampleWebhook("wh-orpheline", base.Add(time.Second))
	if err := repo.Save(orpheline); err != nil {
		t.Fatalf("Save orpheline: %v", err)
	}

	n, err := repo.DeleteByPayment("pay-a", "pay-a", "", "pay-a")
	if err != nil {
		t.Fatalf("DeleteByPayment: %v", err)
	}
	if n != 1 {
		t.Errorf("supprimes = %d, veut 1", n)
	}
	restants, _ := repo.Recent(10)
	if len(restants) != 1 || restants[0].ID != "wh-orpheline" {
		t.Errorf("l'orpheline devait survivre, restants = %+v", restants)
	}

	// DeleteAttached l'épargne aussi : c'est ce qui la distingue de
	// DeleteAll.
	if n, err = repo.DeleteAttached(); err != nil || n != 0 {
		t.Errorf("DeleteAttached = (%d, %v), veut (0, nil)", n, err)
	}
	if restants, _ = repo.Recent(10); len(restants) != 1 {
		t.Errorf("DeleteAttached a emporte l'orpheline")
	}
}
