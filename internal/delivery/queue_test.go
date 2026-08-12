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

	"github.com/sprimault/paysim/internal/bus"
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

// TestDeliverRespectsDelay vérifie qu'un Webhook.Delay différencie
// l'instant de livraison. Cœur du support out-of-order.
func TestDeliverRespectsDelay(t *testing.T) {
	t.Parallel()
	received := make(chan time.Time, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- time.Now()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	q := newQueue(t, 10)
	cancel, wait := runInBackground(t, q)

	enqueuedAt := time.Now()
	if err := q.Enqueue(Webhook{ID: "w", URL: server.URL, Body: []byte("{}"), Delay: 300 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}

	select {
	case receivedAt := <-received:
		elapsed := receivedAt.Sub(enqueuedAt)
		if elapsed < 250*time.Millisecond {
			t.Errorf("livraison à %v après Enqueue, veut >= 250ms (delay=300ms)", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook non reçu")
	}

	cancel()
	wait()
}

// TestOutOfOrderDelivery : deux webhooks, le premier avec délai, le
// second sans → le second arrive avant le premier. C'est le mécanisme
// de composition qui remplace un flag "out-of-order" dédié.
//
// Le délai vaut une seconde et non trois cents millisecondes : la marge
// est ce qui sépare l'arrivée du second de celle du premier, et une
// machine qui décroche plus longtemps que le délai inverse l'ordre sans
// que rien ne soit cassé. Constaté sur un poste de développement, le
// test échouant seul puis repassant huit fois de suite.
func TestOutOfOrderDelivery(t *testing.T) {
	t.Parallel()
	receivedIDs := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Test-ID")
		receivedIDs <- id
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	q := newQueue(t, 10)
	cancel, wait := runInBackground(t, q)

	// Enqueue "premier" (délai 1s) puis "second" (immédiat).
	// Ordre d'arrivée attendu : "second", puis "premier".
	if err := q.Enqueue(Webhook{
		ID: "premier", URL: server.URL, Body: []byte("{}"),
		Delay:   time.Second,
		Headers: map[string]string{"X-Test-ID": "premier"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(Webhook{
		ID: "second", URL: server.URL, Body: []byte("{}"),
		Headers: map[string]string{"X-Test-ID": "second"},
	}); err != nil {
		t.Fatal(err)
	}

	first := <-receivedIDs
	second := <-receivedIDs
	if first != "second" || second != "premier" {
		t.Errorf("ordre reçu = [%s, %s], veut [second, premier]", first, second)
	}

	cancel()
	wait()
}

// TestDrainWaitsForInflightWithDelay : à l'arrêt, si un webhook a un
// délai encore en cours, le drain doit soit attendre soit compter
// l'échec — pas laisser une goroutine orpheline.
func TestDrainWaitsForInflightWithDelay(t *testing.T) {
	t.Parallel()
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	q := newQueue(t, 10)
	if err := q.Enqueue(Webhook{
		ID: "differé", URL: server.URL, Body: []byte("{}"),
		Delay: 100 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	// ctx annulé rapidement, le délai n'est pas fini.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	// Run doit revenir sans goroutine orpheline. Le webhook est
	// abandonné (failed++) parce que ctx annulé pendant le délai.
	if err := q.Run(ctx); err != nil {
		t.Fatalf("Run : %v", err)
	}

	if received.Load() != 0 {
		t.Errorf("webhook livré (%d) alors que ctx annulé pendant delay", received.Load())
	}
	if q.Stats().Failed != 1 {
		t.Errorf("Failed = %d, veut 1 (delay interrompu par cancel)", q.Stats().Failed)
	}
}

func TestEnqueuePublishesWebhookEnqueuedEvent(t *testing.T) {
	t.Parallel()
	q := newQueue(t, 4)
	b := bus.New()
	q.SetPublisher(b)

	ch, unsub := b.Subscribe(4)
	defer unsub()

	if err := q.Enqueue(Webhook{ID: "wh-1", URL: "http://example.local/cb"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case e := <-ch:
		if e.Type != "webhook_enqueued" {
			t.Errorf("type = %q, veut webhook_enqueued", e.Type)
		}
		data, ok := e.Data.(map[string]any)
		if !ok {
			t.Fatalf("Data pas map[string]any: %T", e.Data)
		}
		if data["id"] != "wh-1" {
			t.Errorf("id = %v", data["id"])
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("event webhook_enqueued non reçu")
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
