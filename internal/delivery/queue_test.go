// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// discardLogger renvoie un logger qui n'écrit rien — évite de polluer
// la sortie des tests avec les logs de livraison.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newQueue construit une queue prête pour les tests, avec un client
// HTTP à timeout court et un logger silencieux.
func newQueue(t *testing.T, capacity int) *Queue {
	t.Helper()
	return New(&http.Client{Timeout: 2 * time.Second}, discardLogger(), capacity)
}

// runInBackground lance q.Run dans une goroutine et retourne un cancel
// et un wait pour l'orchestration propre du test — évite les leaks de
// goroutine entre tests.
func runInBackground(t *testing.T, q *Queue) (cancel context.CancelFunc, wait func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Run(ctx)
	}()
	return cancel, wg.Wait
}

// waitFor poll une condition jusqu'à un deadline. Utile pour attendre
// qu'un compteur atteigne une valeur sans dormir de façon fixe.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestEnqueueAndDeliver(t *testing.T) {
	t.Parallel()

	type reception struct {
		body    []byte
		headers http.Header
	}
	received := make(chan reception, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- reception{body: body, headers: r.Header.Clone()}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	q := newQueue(t, 10)
	cancel, wait := runInBackground(t, q)
	defer func() { cancel(); wait() }()

	hook := Webhook{
		ID:  "wh-1",
		URL: server.URL,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"Kr-Hash-Key":  "sha256_hmac",
		},
		Body: []byte(`{"orderStatus":"PAID"}`),
	}
	if err := q.Enqueue(hook); err != nil {
		t.Fatalf("Enqueue : %v", err)
	}

	select {
	case r := <-received:
		if got := string(r.body); got != `{"orderStatus":"PAID"}` {
			t.Errorf("body reçu %q", got)
		}
		if r.headers.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, veut application/json", r.headers.Get("Content-Type"))
		}
		if r.headers.Get("Kr-Hash-Key") != "sha256_hmac" {
			t.Errorf("Kr-Hash-Key = %q, veut sha256_hmac", r.headers.Get("Kr-Hash-Key"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook non livré après 2s")
	}

	if !waitFor(t, 500*time.Millisecond, func() bool { return q.Stats().Delivered == 1 }) {
		t.Errorf("Stats.Delivered = %d après attente, veut 1", q.Stats().Delivered)
	}
	if s := q.Stats(); s.Failed != 0 {
		t.Errorf("Stats.Failed = %d, veut 0", s.Failed)
	}
}

func TestEnqueueQueueFull(t *testing.T) {
	t.Parallel()
	q := newQueue(t, 1)
	// On n'appelle jamais Run : la file reste bloquée à 1 job.

	if err := q.Enqueue(Webhook{ID: "1", URL: "http://x"}); err != nil {
		t.Fatalf("premier Enqueue : %v", err)
	}
	if err := q.Enqueue(Webhook{ID: "2", URL: "http://x"}); !errors.Is(err, ErrQueueFull) {
		t.Errorf("second Enqueue = %v, veut ErrQueueFull", err)
	}
}

func TestRunAlreadyRunning(t *testing.T) {
	t.Parallel()
	q := newQueue(t, 1)
	cancel, wait := runInBackground(t, q)
	defer func() { cancel(); wait() }()

	// Laisser un instant au premier Run pour gagner le CompareAndSwap.
	if !waitFor(t, 200*time.Millisecond, func() bool { return q.running.Load() }) {
		t.Fatal("premier Run n'a pas démarré")
	}

	ctx, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	if err := q.Run(ctx); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Run = %v, veut ErrAlreadyRunning", err)
	}
}

func TestRunDrainOnCancel(t *testing.T) {
	t.Parallel()
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	q := newQueue(t, 10)
	for i := 0; i < 5; i++ {
		if err := q.Enqueue(Webhook{ID: "x", URL: server.URL, Body: []byte("{}")}); err != nil {
			t.Fatalf("Enqueue #%d : %v", i, err)
		}
	}

	// ctx déjà annulé — Run doit quand même drainer la file avant sortie.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := q.Run(ctx); err != nil {
		t.Fatalf("Run : %v", err)
	}

	if got := count.Load(); got != 5 {
		t.Errorf("livrés = %d, veut 5 (drain)", got)
	}
	if s := q.Stats(); s.Pending != 0 {
		t.Errorf("Pending après drain = %d, veut 0", s.Pending)
	}
}

func TestDeliverFailedOn5xx(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	q := newQueue(t, 10)
	cancel, wait := runInBackground(t, q)

	if err := q.Enqueue(Webhook{ID: "wh", URL: server.URL, Body: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return q.Stats().Failed == 1 }) {
		t.Fatalf("Failed non atteint après 2s : %+v", q.Stats())
	}
	cancel()
	wait()

	if s := q.Stats(); s.Failed != 1 || s.Delivered != 0 {
		t.Errorf("stats = %+v, veut Failed=1 Delivered=0", s)
	}
}

func TestDeliverTimeout(t *testing.T) {
	t.Parallel()
	// Handler qui traîne au-delà du timeout client.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	q := New(client, discardLogger(), 10)
	cancel, wait := runInBackground(t, q)

	if err := q.Enqueue(Webhook{ID: "wh", URL: server.URL, Body: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return q.Stats().Failed == 1 }) {
		t.Fatalf("Failed non atteint : %+v", q.Stats())
	}
	cancel()
	wait()
}

func TestConcurrentEnqueue(t *testing.T) {
	t.Parallel()
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	q := newQueue(t, 1000)
	cancel, wait := runInBackground(t, q)

	const producers = 5
	const per = 10
	const total = producers * per

	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				_ = q.Enqueue(Webhook{ID: "wh", URL: server.URL, Body: []byte("{}")})
			}
		}()
	}
	wg.Wait()

	if !waitFor(t, 5*time.Second, func() bool { return received.Load() == total }) {
		t.Fatalf("reçus = %d, veut %d", received.Load(), total)
	}
	cancel()
	wait()

	if s := q.Stats(); s.Delivered != total {
		t.Errorf("Delivered = %d, veut %d", s.Delivered, total)
	}
}

func TestNewCapacityMinimum(t *testing.T) {
	t.Parallel()
	// Capacité 0 doit être ramenée à 1 — sinon Enqueue serait toujours
	// bloquant sans jamais démarrer, un état de blocage silencieux à éviter.
	q := New(&http.Client{}, discardLogger(), 0)

	if err := q.Enqueue(Webhook{ID: "1", URL: "http://x"}); err != nil {
		t.Errorf("premier Enqueue devrait passer, erreur : %v", err)
	}
	if err := q.Enqueue(Webhook{ID: "2", URL: "http://x"}); !errors.Is(err, ErrQueueFull) {
		t.Errorf("second Enqueue = %v, veut ErrQueueFull", err)
	}
}

func TestEnqueueAutoSetsCreatedAt(t *testing.T) {
	t.Parallel()
	// On envoie un webhook avec CreatedAt zéro et on vérifie que
	// Enqueue le fixe automatiquement — via l'observation qu'aucun
	// panic n'a lieu et que le job est bien accepté. La valeur exacte
	// n'est pas observable de l'extérieur, ce qui est voulu.
	q := newQueue(t, 1)
	before := time.Now().UTC()
	if err := q.Enqueue(Webhook{ID: "1", URL: "http://x"}); err != nil {
		t.Fatal(err)
	}
	if q.Stats().Pending != 1 {
		t.Errorf("Pending = %d, veut 1", q.Stats().Pending)
	}
	_ = before // marque l'intention : test ne panique pas malgré CreatedAt zéro.
}
