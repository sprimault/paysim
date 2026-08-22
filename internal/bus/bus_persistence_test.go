// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package bus

import (
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

// fakeEventRepo est une impl mémoire de store.EventRepository pour tests.
// Concurrent-safe car le worker persistance appelle Save depuis une goroutine
// séparée pendant que le test lit avec All/Since.
type fakeEventRepo struct {
	mu      sync.Mutex
	records []store.EventRecord
	saveErr error
}

func (r *fakeEventRepo) Save(rec store.EventRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.records = append(r.records, rec)
	return nil
}

func (r *fakeEventRepo) Since(lastID uint64, limit int) ([]store.EventRecord, error) {
	if limit <= 0 {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sorted := append([]store.EventRecord{}, r.records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	out := make([]store.EventRecord, 0, len(sorted))
	for _, rec := range sorted {
		if rec.ID > lastID {
			out = append(out, rec)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *fakeEventRepo) DeleteBefore(id uint64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.records[:0]
	deleted := 0
	for _, rec := range r.records {
		if rec.ID <= id {
			deleted++
			continue
		}
		kept = append(kept, rec)
	}
	r.records = kept
	return deleted, nil
}

func (r *fakeEventRepo) Close() error { return nil }

func (r *fakeEventRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// TestWithPersistenceSavesEachEvent : chaque Publish transite vers le repo
// après drain. Close attend le drain complet du worker.
func TestWithPersistenceSavesEachEvent(t *testing.T) {
	t.Parallel()
	repo := &fakeEventRepo{}
	b := New().WithPersistence(repo, nil)

	for i := 0; i < 10; i++ {
		b.Publish(Event{Type: "x", At: time.Now(), Data: map[string]any{"i": i}})
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := repo.count(); got != 10 {
		t.Errorf("repo.count() = %d, veut 10", got)
	}
	all, _ := repo.Since(0, 1000)
	for i, rec := range all {
		if rec.ID != uint64(i+1) {
			t.Errorf("[%d].ID = %d, veut %d", i, rec.ID, i+1)
		}
	}
}

// TestPersistenceOptional : sans WithPersistence, aucun repo touché.
func TestPersistenceOptional(t *testing.T) {
	t.Parallel()
	repo := &fakeEventRepo{}
	b := New()
	b.Publish(Event{Type: "x"})
	// Pas de Close nécessaire — persistance non activée.
	if repo.count() != 0 {
		t.Errorf("repo touché sans WithPersistence")
	}
}

// TestWithPersistenceIsIdempotent : deuxième appel ignoré (sync.Once).
func TestWithPersistenceIsIdempotent(t *testing.T) {
	t.Parallel()
	repo1 := &fakeEventRepo{}
	repo2 := &fakeEventRepo{}
	b := New().WithPersistence(repo1, nil).WithPersistence(repo2, nil)
	b.Publish(Event{Type: "x"})
	_ = b.Close()

	if repo1.count() != 1 {
		t.Errorf("repo1 = %d, veut 1 (premier appel gagne)", repo1.count())
	}
	if repo2.count() != 0 {
		t.Errorf("repo2 = %d, veut 0 (deuxième appel ignoré)", repo2.count())
	}
}

// TestPersistenceSaveErrorNonFatal : une erreur de Save ne casse pas le bus.
func TestPersistenceSaveErrorNonFatal(t *testing.T) {
	t.Parallel()
	repo := &fakeEventRepo{saveErr: errors.New("disk full")}
	b := New().WithPersistence(repo, nil)
	for i := 0; i < 5; i++ {
		b.Publish(Event{Type: "x"})
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close malgré erreur Save : %v", err)
	}
	// Subscribers reçoivent quand même — Publish n'est pas bloqué.
}

// TestCloseNilBusSafe : Close sur Bus nil ou sans persistance = no-op.
func TestCloseNilBusSafe(t *testing.T) {
	t.Parallel()
	var nilBus *Bus
	if err := nilBus.Close(); err != nil {
		t.Errorf("Close nil : %v", err)
	}
	b := New() // sans WithPersistence
	if err := b.Close(); err != nil {
		t.Errorf("Close sans persist : %v", err)
	}
}

// TestSnapshotSinceRingEmptyFallsBackToRepo : après restart simulé, le ring
// est vide mais le repo contient les events pré-restart. SnapshotSince doit
// servir depuis le repo.
func TestSnapshotSinceRingEmptyFallsBackToRepo(t *testing.T) {
	t.Parallel()
	repo := &fakeEventRepo{}
	// Simule des events pré-restart directement dans le repo.
	for i := uint64(1); i <= 3; i++ {
		_ = repo.Save(store.EventRecord{
			ID:       i,
			Type:     "payment_created",
			At:       time.Now(),
			DataJSON: `{"n":` + string(rune('0'+i)) + `}`,
		})
	}

	// Nouveau bus, persistance activée, ring vide (aucun Publish après start).
	b := New().WithPersistence(repo, nil)
	defer func() { _ = b.Close() }()

	events, high := b.SnapshotSince(0)
	if len(events) != 3 {
		t.Fatalf("events len=%d, veut 3", len(events))
	}
	if high != 3 {
		t.Errorf("highWater=%d, veut 3", high)
	}
	if events[0].ID != 1 || events[2].ID != 3 {
		t.Errorf("IDs : %+v", events)
	}
}

// TestSnapshotSinceMergesRepoAndRing : lastID plus ancien que oldestBuffered
// et repo actif — les events manquants sont rapatriés depuis le repo puis
// concaténés avec le ring, sans doublon ni trou.
//
// On injecte directement dans le ring buffer et le repo pour simuler un
// ring qui a débordé (impossible à provoquer via Publish avec bufferCap=10_000).
func TestSnapshotSinceMergesRepoAndRing(t *testing.T) {
	t.Parallel()
	repo := &fakeEventRepo{}
	// Repo contient toute l'histoire : IDs 1..10.
	for i := uint64(1); i <= 10; i++ {
		_ = repo.Save(store.EventRecord{ID: i, Type: "e", At: time.Now(), DataJSON: `{}`})
	}

	// Ring ne couvre que les 3 derniers (simule un ring qui a débordé
	// et perdu 1..7 côté mémoire, mais le repo les a conservés).
	b := New()
	b.persistRepo = repo
	b.buffer = []Event{
		{ID: 8, Type: "e"},
		{ID: 9, Type: "e"},
		{ID: 10, Type: "e"},
	}
	b.counter.Store(10)

	events, high := b.SnapshotSince(0)

	if high != 10 {
		t.Errorf("highWater=%d, veut 10", high)
	}
	seen := map[uint64]int{}
	for _, e := range events {
		seen[e.ID]++
	}
	for id := uint64(1); id <= 10; id++ {
		if seen[id] != 1 {
			t.Errorf("ID %d compté %d fois, veut 1", id, seen[id])
		}
	}

	// Ordre : repo d'abord (IDs croissants), ring ensuite (croissants).
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Errorf("ordre cassé à [%d] : %d après %d", i, events[i].ID, events[i-1].ID)
		}
	}
}

// TestPersistenceQueueSaturationDrops : quand le worker est en retard et
// que la queue déborde, Publish ne bloque pas.
func TestPersistenceQueueSaturationDrops(t *testing.T) {
	t.Parallel()
	// slowRepo bloque chaque Save pendant une courte durée — la queue
	// se remplit vite. Un Publish burst doit rester non-bloquant.
	slow := &slowFakeRepo{delay: 20 * time.Millisecond}
	b := New().WithPersistence(slow, nil)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			b.Publish(Event{Type: "burst"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish bloqué par saturation queue persistance")
	}
	// Ne pas Close ici : le worker prendrait 40 secondes à drainer.
	// La goroutine reste en vie jusqu'à la fin du test.
}

type slowFakeRepo struct {
	delay time.Duration
	mu    sync.Mutex
	n     int
}

func (r *slowFakeRepo) Save(store.EventRecord) error {
	time.Sleep(r.delay)
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	return nil
}
func (r *slowFakeRepo) Since(uint64, int) ([]store.EventRecord, error) { return nil, nil }
func (r *slowFakeRepo) DeleteBefore(uint64) (int, error)          { return 0, nil }
func (r *slowFakeRepo) Close() error                              { return nil }
