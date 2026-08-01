// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"path/filepath"
	"testing"
	"time"

	sqlitepkg "github.com/sprimault/paysim/internal/store/sqlite"
)

// runHistoryContract exécute le même scénario sur une HistoryStore —
// garantit que MemoryHistory et SQLiteHistory sont interchangeables
// au bit près.
func runHistoryContract(t *testing.T, h HistoryStore) {
	t.Helper()
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	// Ajout de 3 records dans l'ordre chronologique.
	for i, id := range []string{"wh-1", "wh-2", "wh-3"} {
		rec := WebhookRecord{
			Webhook: Webhook{
				ID:        id,
				URL:       "http://x",
				Headers:   map[string]string{"h": "v"},
				Body:      []byte("payload-" + id),
				Attempts:  1,
				CreatedAt: base.Add(time.Duration(i) * time.Second),
			},
			Status:      "delivered",
			StatusCode:  200,
			CompletedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := h.Add(rec); err != nil {
			t.Fatalf("Add %s: %v", id, err)
		}
	}

	// Recent doit renvoyer plus récent d'abord.
	recent := h.Recent(10)
	if len(recent) != 3 {
		t.Fatalf("Recent len = %d, veut 3", len(recent))
	}
	if recent[0].Webhook.ID != "wh-3" || recent[2].Webhook.ID != "wh-1" {
		t.Errorf("ordre = %s..%s, veut wh-3..wh-1",
			recent[0].Webhook.ID, recent[2].Webhook.ID)
	}

	// ByID direct.
	got, ok := h.ByID("wh-2")
	if !ok || got.Webhook.ID != "wh-2" {
		t.Errorf("ByID = %+v, ok=%v", got, ok)
	}
	if string(got.Webhook.Body) != "payload-wh-2" {
		t.Errorf("body corrompu")
	}

	// ByID inconnu.
	_, ok = h.ByID("inconnu")
	if ok {
		t.Error("ByID inconnu = ok true, veut false")
	}

	// DeleteAll purge tout.
	n, err := h.DeleteAll()
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if n != 3 {
		t.Errorf("deleted = %d, veut 3", n)
	}
	if r := h.Recent(10); len(r) != 0 {
		t.Errorf("apres purge : Recent len = %d", len(r))
	}
}

func TestMemoryHistoryContract(t *testing.T) {
	t.Parallel()
	runHistoryContract(t, NewMemoryHistory())
}

func TestSQLiteHistoryContract(t *testing.T) {
	t.Parallel()
	db, err := sqlitepkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo, err := sqlitepkg.NewWebhooksRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	runHistoryContract(t, NewSQLiteHistory(repo))
}

func TestMemoryHistoryRingWrapping(t *testing.T) {
	t.Parallel()
	h := NewMemoryHistory()
	// Remplir au-delà de la capacité — les plus anciens sont écrasés.
	for i := 0; i < historyCap+50; i++ {
		_ = h.Add(WebhookRecord{
			Webhook: Webhook{ID: "wh-" + string(rune('a'+i%26))},
		})
	}
	// Recent ne doit pas dépasser la capacité.
	if got := h.Recent(historyCap + 100); len(got) != historyCap {
		t.Errorf("Recent len = %d, veut %d", len(got), historyCap)
	}
}
