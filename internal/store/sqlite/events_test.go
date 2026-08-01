// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

func buildEventsRepo(t *testing.T) *EventsRepository {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	repo, err := NewEventsRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewEventsRepository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func sampleEvent(id uint64, typ string, at time.Time, data string) store.EventRecord {
	return store.EventRecord{ID: id, Type: typ, At: at, DataJSON: data}
}

func TestSaveAndSinceEvents(t *testing.T) {
	t.Parallel()
	repo := buildEventsRepo(t)
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	for i := uint64(1); i <= 5; i++ {
		if err := repo.Save(sampleEvent(i, "payment_created", base.Add(time.Duration(i)*time.Millisecond),
			`{"uuid":"u"}`)); err != nil {
			t.Fatalf("Save(%d): %v", i, err)
		}
	}

	all, err := repo.Since(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("Since(0) len=%d, veut 5", len(all))
	}
	for i, r := range all {
		if r.ID != uint64(i+1) {
			t.Errorf("ordre : [%d].ID = %d, veut %d", i, r.ID, i+1)
		}
	}
	if !all[0].At.Equal(base.Add(time.Millisecond)) {
		t.Errorf("At roundtrip : %v", all[0].At)
	}

	partial, err := repo.Since(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial) != 2 || partial[0].ID != 4 || partial[1].ID != 5 {
		t.Errorf("Since(3) = %+v", partial)
	}
}

func TestSaveRejectsIDZero(t *testing.T) {
	t.Parallel()
	repo := buildEventsRepo(t)
	err := repo.Save(sampleEvent(0, "x", time.Now(), `{}`))
	if err == nil {
		t.Error("Save(ID=0) : err = nil, attendu non-nil")
	}
}

func TestSaveIsIdempotent(t *testing.T) {
	t.Parallel()
	repo := buildEventsRepo(t)
	rec := sampleEvent(42, "webhook_delivered", time.Now().UTC(), `{"n":1}`)
	if err := repo.Save(rec); err != nil {
		t.Fatal(err)
	}
	// Retentative (rebond persist worker, replay, etc.) — pas d'erreur.
	if err := repo.Save(rec); err != nil {
		t.Errorf("Save rejeu : %v", err)
	}
	got, _ := repo.Since(0)
	if len(got) != 1 {
		t.Errorf("Since(0) len=%d apres rejeu, veut 1", len(got))
	}
}

func TestDeleteBefore(t *testing.T) {
	t.Parallel()
	repo := buildEventsRepo(t)
	base := time.Now().UTC()
	for i := uint64(1); i <= 10; i++ {
		_ = repo.Save(sampleEvent(i, "e", base, `{}`))
	}

	n, err := repo.DeleteBefore(5)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("DeleteBefore(5) supprimé=%d, veut 5", n)
	}
	rest, _ := repo.Since(0)
	if len(rest) != 5 || rest[0].ID != 6 {
		t.Errorf("apres purge : %+v", rest)
	}
}

func TestSinceEmpty(t *testing.T) {
	t.Parallel()
	repo := buildEventsRepo(t)
	got, err := repo.Since(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Since(0) sur repo vide = %d, veut 0", len(got))
	}
}
