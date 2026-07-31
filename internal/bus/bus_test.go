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
