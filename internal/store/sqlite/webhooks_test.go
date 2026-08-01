// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
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
