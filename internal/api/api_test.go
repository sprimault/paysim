// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/providers/payzen"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// setup construit un environnement complet : store peuplé + queue +
// bus + handler API branché.
func setup(t *testing.T, token string) (*httptest.Server, *payzen.Store, *delivery.Queue, *bus.Bus) {
	t.Helper()
	logger := discardLogger()
	store := payzen.NewStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	queue.SetPublisher(b)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = queue.Run(ctx)
	}()

	handler := NewHandler(Deps{
		Store:     store,
		Queue:     queue,
		Publisher: b,
		Logger:    logger,
		Token:     token,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		cancel()
		wg.Wait()
	})
	return server, store, queue, b
}

// addPayment insère un paiement directement dans le store, sans passer
// par les endpoints REST V4 — plus rapide pour tester l'API UI.
func addPayment(t *testing.T, store *payzen.Store, uuid, orderID string, amount int64) *payzen.Transaction {
	t.Helper()
	_ = amount // paramètre gardé pour lisibilité des appels, valeur fixe pour simplifier
	p, err := domain.New(uuid, 1500, "EUR")
	if err != nil {
		t.Fatalf("domain.New : %v", err)
	}
	now := time.Now().UTC()
	tx := &payzen.Transaction{
		FormToken: "tok-" + uuid,
		UUID:      uuid,
		OrderID:   orderID,
		Amount:    1500,
		Currency:  "EUR",
		Payment:   p,
		CreatedAt: now,
		UpdatedAt: now,
	}
	store.Save(tx)
	return tx
}

func TestListPaymentsEmpty(t *testing.T) {
	t.Parallel()
	server, _, _, _ := setup(t, "")

	resp, err := http.Get(server.URL + "/paysim/api/v1/payments")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, veut 200", resp.StatusCode)
	}
	var got []PaymentSummary
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("payments = %d, veut 0 (vide)", len(got))
	}
}

func TestListPaymentsReturnsAll(t *testing.T) {
	t.Parallel()
	server, store, _, _ := setup(t, "")
	addPayment(t, store, "uuid-a", "order-a", 1500)
	addPayment(t, store, "uuid-b", "order-b", 2000)

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments")
	defer func() { _ = resp.Body.Close() }()
	var got []PaymentSummary
	_ = json.NewDecoder(resp.Body).Decode(&got)

	if len(got) != 2 {
		t.Fatalf("payments = %d, veut 2", len(got))
	}
	// Ordre non garanti — vérifier par map.
	seen := map[string]bool{}
	for _, p := range got {
		seen[p.OrderID] = true
	}
	if !seen["order-a"] || !seen["order-b"] {
		t.Errorf("orderIds reçus : %+v", got)
	}
}

func TestGetPaymentByUUID(t *testing.T) {
	t.Parallel()
	server, store, _, _ := setup(t, "")
	addPayment(t, store, "uuid-x", "order-x", 500)

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments/uuid-x")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got PaymentDetail
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.UUID != "uuid-x" || got.OrderID != "order-x" {
		t.Errorf("payment = %+v", got.PaymentSummary)
	}
	if len(got.Events) == 0 {
		t.Error("Events vide, doit contenir au moins EventCreated")
	}
}

func TestGetPaymentUnknown(t *testing.T) {
	t.Parallel()
	server, _, _, _ := setup(t, "")
	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments/inexistant")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, veut 404", resp.StatusCode)
	}
}

func TestListWebhooksEmpty(t *testing.T) {
	t.Parallel()
	server, _, _, _ := setup(t, "")
	resp, _ := http.Get(server.URL + "/paysim/api/v1/webhooks")
	defer func() { _ = resp.Body.Close() }()
	var got []WebhookEntry
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if len(got) != 0 {
		t.Errorf("webhooks = %d, veut 0", len(got))
	}
}

func TestListWebhooksAfterDelivery(t *testing.T) {
	t.Parallel()
	// Serveur aval qui répond OK.
	aval := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer aval.Close()

	server, _, queue, _ := setup(t, "")
	_ = queue.Enqueue(delivery.Webhook{
		ID:      "wh-1",
		URL:     aval.URL,
		Body:    []byte(`{"x":1}`),
		Headers: map[string]string{"Content-Type": "application/json"},
	})

	// Attendre la livraison — poll jusqu'à ce que la liste ne soit plus vide.
	deadline := time.Now().Add(2 * time.Second)
	var got []WebhookEntry
	for time.Now().Before(deadline) {
		resp, _ := http.Get(server.URL + "/paysim/api/v1/webhooks")
		got = nil
		_ = json.NewDecoder(resp.Body).Decode(&got)
		_ = resp.Body.Close()
		if len(got) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(got) != 1 {
		t.Fatalf("webhooks = %d, veut 1", len(got))
	}
	if got[0].ID != "wh-1" || got[0].Status != "delivered" || got[0].StatusCode != 200 {
		t.Errorf("webhook = %+v", got[0])
	}
}

func TestBearerRequiredWhenTokenConfigured(t *testing.T) {
	t.Parallel()
	server, _, _, _ := setup(t, "secret-bearer")

	// Sans token → 401.
	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("sans bearer = %d, veut 401", resp.StatusCode)
	}

	// Avec bon token → 200.
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/paysim/api/v1/payments", nil)
	req.Header.Set("Authorization", "Bearer secret-bearer")
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("avec bearer = %d, veut 200", resp.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// Tests des endpoints write (vertical 2 phase 3)
// -----------------------------------------------------------------------------

func TestReplayWebhookReEnqueues(t *testing.T) {
	t.Parallel()
	// Serveur aval qui compte les POSTs.
	var count atomic.Int32
	aval := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer aval.Close()

	server, _, queue, _ := setup(t, "")

	// 1. Enqueue initial + attente livraison.
	origBody := []byte(`{"orderStatus":"PAID"}`)
	_ = queue.Enqueue(delivery.Webhook{
		ID:      "wh-original",
		URL:     aval.URL,
		Body:    origBody,
		Headers: map[string]string{"Content-Type": "application/json"},
	})
	// count.Add(1) est fait dans le handler aval AVANT que finish()
	// n'ajoute l'entrée à queue.Recent(). Attendre les deux
	// conditions évite un race entre le POST /replay et la peuplement
	// de l'historique.
	deadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		if count.Load() >= 1 {
			for _, r := range queue.Recent(50) {
				if r.Webhook.ID == "wh-original" {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatalf("livraison initiale : count=%d, historique non peuplé", count.Load())
	}

	// 2. Rejeu via API.
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/webhooks/wh-original/replay", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("replay status = %d, veut 202", resp.StatusCode)
	}
	var body ReplayWebhookResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if !strings.HasPrefix(body.NewDeliveryID, "replay-wh-original-") {
		t.Errorf("NewDeliveryID = %q, doit commencer par replay-wh-original-", body.NewDeliveryID)
	}

	// 3. Le POST doit arriver — count passe à 2.
	deadline = time.Now().Add(2 * time.Second)
	for count.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if count.Load() != 2 {
		t.Errorf("apres replay : count = %d, veut 2", count.Load())
	}
}

func TestReplayWebhookUnknown(t *testing.T) {
	t.Parallel()
	server, _, _, _ := setup(t, "")

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/webhooks/inexistant/replay", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, veut 404", resp.StatusCode)
	}
}

func TestSimulatePaymentBrowserReturn(t *testing.T) {
	t.Parallel()
	// Setup avec un vrai payzenHandler (nécessaire pour Simulate).
	logger := discardLogger()
	store := payzen.NewStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	queue.SetPublisher(b)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); wg.Wait() })

	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{
		HMACKey:   "test-hmac",
		Publisher: b,
	})

	handler := NewHandler(Deps{
		Store:         store,
		Queue:         queue,
		Publisher:     b,
		Logger:        logger,
		PayzenHandler: ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Créer une transaction (avec ReturnURL stockée).
	tx := addPayment(t, store, "uuid-sim", "order-sim", 500)
	tx.ReturnURL = "http://localhost:1/discard" // URL bidon, on ne vérifie que la réponse API
	store.Save(tx)

	body, _ := json.Marshal(SimulatePaymentRequest{
		Outcome: "PAID",
	})
	resp, err := http.Post(
		server.URL+"/paysim/api/v1/payments/uuid-sim/simulate",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, veut 202", resp.StatusCode)
	}
	var out SimulatePaymentResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.DeliveryID == "" || out.KrHash == "" {
		t.Errorf("response incomplete = %+v", out)
	}
	if out.Channel != "browserReturn" {
		t.Errorf("Channel = %q, veut browserReturn (défaut)", out.Channel)
	}
}

func TestSimulatePaymentUnknownUUID(t *testing.T) {
	t.Parallel()
	logger := discardLogger()
	store := payzen.NewStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{HMACKey: "k", Publisher: b})
	handler := NewHandler(Deps{
		Store: store, Queue: queue, Publisher: b, Logger: logger, PayzenHandler: ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	body, _ := json.Marshal(SimulatePaymentRequest{Outcome: "PAID"})
	resp, _ := http.Post(
		server.URL+"/paysim/api/v1/payments/inexistant/simulate",
		"application/json", bytes.NewReader(body))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, veut 404", resp.StatusCode)
	}
}

func TestSimulatePaymentInvalidChannel(t *testing.T) {
	t.Parallel()
	logger := discardLogger()
	store := payzen.NewStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{HMACKey: "k", Publisher: b})
	handler := NewHandler(Deps{
		Store: store, Queue: queue, Publisher: b, Logger: logger, PayzenHandler: ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	addPayment(t, store, "uuid-x", "order-x", 100)

	body, _ := json.Marshal(SimulatePaymentRequest{Outcome: "PAID", Channel: "unknown-channel"})
	resp, _ := http.Post(
		server.URL+"/paysim/api/v1/payments/uuid-x/simulate",
		"application/json", bytes.NewReader(body))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400", resp.StatusCode)
	}
}

func TestSSEStreamReceivesEvents(t *testing.T) {
	t.Parallel()
	server, _, _, b := setup(t, "")

	// Client SSE.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/paysim/api/v1/events/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}

	// Publier un événement — délai court pour laisser le subscribe s'établir.
	time.Sleep(100 * time.Millisecond)
	b.Publish(bus.Event{Type: "test_event", At: time.Now(), Data: map[string]string{"hello": "world"}})

	// Lire jusqu'à trouver une ligne data:.
	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(1500 * time.Millisecond)
	var payload string
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "data: ") {
			payload = strings.TrimPrefix(line, "data: ")
			payload = strings.TrimRight(payload, "\r\n")
			break
		}
	}
	if payload == "" {
		t.Fatal("aucune ligne data: reçue via SSE")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("payload SSE non JSON : %v — payload = %q", err, payload)
	}
	if got["type"] != "test_event" {
		t.Errorf("type = %v", got["type"])
	}
}

// TestSSELastEventIDReplay vérifie qu'un client reprenant contact
// avec un header Last-Event-ID reçoit le catch-up depuis le ring
// buffer sans doublon ni trou, puis continue en live.
func TestSSELastEventIDReplay(t *testing.T) {
	t.Parallel()
	logger := discardLogger()
	store := payzen.NewStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	handler := NewHandler(Deps{Store: store, Queue: queue, Publisher: b, Logger: logger})
	server := httptest.NewServer(handler)
	defer server.Close()

	// Publier 3 events AVANT la connexion : ils prendront IDs 1..3.
	b.Publish(bus.Event{Type: "pre_1", At: time.Now()})
	b.Publish(bus.Event{Type: "pre_2", At: time.Now()})
	b.Publish(bus.Event{Type: "pre_3", At: time.Now()})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/paysim/api/v1/events/stream", nil)
	// Simule une reconnexion : le client a déjà vu jusqu'à l'ID 1.
	req.Header.Set("Last-Event-ID", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Publier un event live après connexion — deviendra ID 4.
	time.Sleep(150 * time.Millisecond)
	b.Publish(bus.Event{Type: "live_4", At: time.Now()})

	// Lire jusqu'à obtenir 3 lignes id: (attendus 2, 3, 4).
	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(1500 * time.Millisecond)
	var ids []string
	for len(ids) < 3 && time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "id: ") {
			ids = append(ids, strings.TrimSpace(strings.TrimPrefix(line, "id: ")))
		}
	}

	want := []string{"2", "3", "4"}
	if len(ids) != len(want) {
		t.Fatalf("ids reçus = %v, veut %v (catch-up 2,3 puis live 4)", ids, want)
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("ids[%d] = %q, veut %q", i, id, want[i])
		}
	}
}
