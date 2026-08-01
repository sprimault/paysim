// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package bus

import (
	"sync"
	"testing"
	"time"
)

func TestNilBusPublishSafe(t *testing.T) {
	t.Parallel()
	var b *Bus // nil
	b.Publish(Event{Type: "test", At: time.Now()})
	if b.Subscribers() != 0 {
		t.Errorf("Subscribers() nil = %d, veut 0", b.Subscribers())
	}
}

func TestPublishReachesAllSubscribers(t *testing.T) {
	t.Parallel()
	b := New()
	ch1, unsub1 := b.Subscribe(10)
	ch2, unsub2 := b.Subscribe(10)
	defer unsub1()
	defer unsub2()

	b.Publish(Event{Type: "hello"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Type != "hello" {
				t.Errorf("Type reçu = %q", e.Type)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("event non reçu")
		}
	}
}

func TestPublishNonBlockingWhenSubscriberSlow(t *testing.T) {
	t.Parallel()
	b := New()
	// Abonné lent : buffer 1, ne consomme jamais.
	_, unsub := b.Subscribe(1)
	defer unsub()

	// 100 publications doivent toutes retourner sans bloquer,
	// même si l'abonné accumule.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(Event{Type: "spam"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Publish bloqué par abonné lent — contrat non-bloquant violé")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	t.Parallel()
	b := New()
	ch, unsub := b.Subscribe(10)

	if b.Subscribers() != 1 {
		t.Errorf("Subscribers apres Subscribe = %d, veut 1", b.Subscribers())
	}

	unsub()

	if b.Subscribers() != 0 {
		t.Errorf("Subscribers apres unsub = %d, veut 0", b.Subscribers())
	}

	// ch doit être fermé (lecture non-bloquante retourne zero value + false).
	if _, ok := <-ch; ok {
		t.Error("channel non fermé apres unsub")
	}

	// Un Publish apres unsub ne doit pas paniquer.
	b.Publish(Event{Type: "post-unsub"})
}

func TestPublishAssignsMonotonicID(t *testing.T) {
	t.Parallel()
	b := New()
	ch, unsub := b.Subscribe(10)
	defer unsub()

	for i := 0; i < 5; i++ {
		b.Publish(Event{Type: "seq"})
	}

	var last uint64
	for i := 0; i < 5; i++ {
		e := <-ch
		if e.ID <= last {
			t.Fatalf("event %d ID=%d, attendu > %d", i, e.ID, last)
		}
		last = e.ID
	}
}

func TestSnapshotSinceEmpty(t *testing.T) {
	t.Parallel()
	b := New()
	out, high := b.SnapshotSince(0)
	if out != nil || high != 0 {
		t.Errorf("bus vide: out=%v high=%d, veut nil,0", out, high)
	}
}

func TestSnapshotSinceReturnsOnlyAfterCursor(t *testing.T) {
	t.Parallel()
	b := New()
	for i := 0; i < 5; i++ {
		b.Publish(Event{Type: "e"})
	}

	// lastID=0 → tout
	all, high := b.SnapshotSince(0)
	if len(all) != 5 {
		t.Fatalf("SnapshotSince(0) len=%d, veut 5", len(all))
	}
	if high != 5 {
		t.Errorf("highWater=%d, veut 5", high)
	}

	// lastID=3 → derniers 2 (IDs 4 et 5)
	partial, _ := b.SnapshotSince(3)
	if len(partial) != 2 {
		t.Fatalf("SnapshotSince(3) len=%d, veut 2", len(partial))
	}
	if partial[0].ID != 4 || partial[1].ID != 5 {
		t.Errorf("IDs = %d,%d, veut 4,5", partial[0].ID, partial[1].ID)
	}

	// lastID>= highWater → rien
	none, _ := b.SnapshotSince(5)
	if len(none) != 0 {
		t.Errorf("SnapshotSince(5) len=%d, veut 0", len(none))
	}
}

func TestSubscribeThenSnapshotNoDupNoGap(t *testing.T) {
	t.Parallel()
	b := New()

	b.Publish(Event{Type: "before-1"}) // ID 1
	b.Publish(Event{Type: "before-2"}) // ID 2

	// Client "reconnecte" avec lastID=0 : suit la séquence canonique.
	ch, unsub := b.Subscribe(10)
	defer unsub()
	snap, high := b.SnapshotSince(0)

	// Publie 2 events après le snapshot.
	b.Publish(Event{Type: "after-3"}) // ID 3
	b.Publish(Event{Type: "after-4"}) // ID 4

	// Collecte tout ce qui arrive dans le chan avec un petit délai.
	got := append([]Event{}, snap...)
	deadline := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				break loop
			}
			if e.ID > high {
				got = append(got, e)
			}
			if len(got) == 4 {
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if len(got) != 4 {
		t.Fatalf("got %d events, veut 4 (IDs 1..4 sans doublon)", len(got))
	}
	for i, e := range got {
		if e.ID != uint64(i+1) {
			t.Errorf("event %d ID=%d, veut %d", i, e.ID, i+1)
		}
	}
}

func TestConcurrentPublishAndSubscribe(t *testing.T) {
	t.Parallel()
	b := New()
	const publishers = 5
	const perPub = 100

	// Un abonné qui consomme.
	ch, unsub := b.Subscribe(100)
	defer unsub()

	received := 0
	receivedMu := sync.Mutex{}
	done := make(chan struct{})
	go func() {
		for range ch {
			receivedMu.Lock()
			received++
			receivedMu.Unlock()
		}
		close(done)
	}()

	var wg sync.WaitGroup
	for p := 0; p < publishers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perPub; i++ {
				b.Publish(Event{Type: "concurrent"})
			}
		}()
	}
	wg.Wait()

	// Attendre que le consommateur draine ce qu'il peut.
	time.Sleep(100 * time.Millisecond)
	unsub()
	<-done

	// On accepte des drops (buffer 100 vs 500 émis). Simplement, pas
	// de panic ni de deadlock — le detecteur -race doit passer.
	receivedMu.Lock()
	got := received
	receivedMu.Unlock()
	if got == 0 {
		t.Error("aucun event reçu apres publish concurrent")
	}
}
