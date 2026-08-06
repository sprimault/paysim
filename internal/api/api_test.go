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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/providers/payzen"
	sqlitepkg "github.com/sprimault/paysim/internal/store/sqlite"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// setup construit un environnement complet : store peuplé + queue +
// bus + handler API branché.
func setup(t *testing.T, token string) (*httptest.Server, payzen.Store, *delivery.Queue, *bus.Bus) {
	t.Helper()
	logger := discardLogger()
	store := payzen.NewMemoryStore()
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
func addPayment(t *testing.T, store payzen.Store, uuid, orderID string, amount int64) *payzen.Transaction {
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
	_ = store.Save(tx)
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

// -----------------------------------------------------------------------------
// DELETE endpoints — 4.3.3
// -----------------------------------------------------------------------------

// doDelete envoie une requête DELETE sur l'URL donnée.
func doDelete(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestDeletePaymentRemovesFromStore(t *testing.T) {
	t.Parallel()
	server, store, _, _ := setup(t, "")
	addPayment(t, store, "uuid-del", "order-x", 500)

	resp := doDelete(t, server.URL+"/paysim/api/v1/payments/uuid-del")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, veut 204", resp.StatusCode)
	}
	// Verifier disparition côté store.
	if got, _ := store.ByUUID("uuid-del"); got != nil {
		t.Error("paiement toujours présent après DELETE")
	}
}

func TestDeletePaymentUnknownReturns204(t *testing.T) {
	t.Parallel()
	// Idempotent : delete d'un UUID inconnu renvoie 204 (l'état
	// demandé est atteint : ce paiement n'existe pas).
	server, _, _, _ := setup(t, "")
	resp := doDelete(t, server.URL+"/paysim/api/v1/payments/inexistant")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, veut 204", resp.StatusCode)
	}
}

func TestDeletePaymentsAll(t *testing.T) {
	t.Parallel()
	server, store, _, _ := setup(t, "")
	addPayment(t, store, "uuid-a", "order-a", 1000)
	addPayment(t, store, "uuid-b", "order-b", 2000)

	resp := doDelete(t, server.URL+"/paysim/api/v1/payments")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, veut 200", resp.StatusCode)
	}
	var body map[string]int
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["deleted"] != 2 {
		t.Errorf("deleted = %d, veut 2", body["deleted"])
	}
	// Store vide.
	if n, _ := store.Len(); n != 0 {
		t.Errorf("Len = %d apres purge, veut 0", n)
	}
}

func TestDeletePaymentsFiltersByProviderInMemoryMode(t *testing.T) {
	t.Parallel()
	// En mode mémoire, seul PayZen est présent — un filtre autre
	// que payzen doit être un no-op (rien à supprimer).
	server, store, _, _ := setup(t, "")
	addPayment(t, store, "uuid-a", "order-a", 1000)

	resp := doDelete(t, server.URL+"/paysim/api/v1/payments?provider=stripe")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, veut 200", resp.StatusCode)
	}
	var body map[string]int
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["deleted"] != 0 {
		t.Errorf("stripe filter en mémoire : deleted = %d, veut 0", body["deleted"])
	}
	// PayZen reste.
	if n, _ := store.Len(); n != 1 {
		t.Errorf("Len apres purge stripe = %d, veut 1", n)
	}
}

func TestDeletePaymentPublishesEvent(t *testing.T) {
	t.Parallel()
	server, store, _, b := setup(t, "")
	addPayment(t, store, "uuid-evt", "order-x", 500)

	events, unsub := b.Subscribe(4)
	defer unsub()

	resp := doDelete(t, server.URL+"/paysim/api/v1/payments/uuid-evt")
	_ = resp.Body.Close()

	select {
	case e := <-events:
		if e.Type != "payment_deleted" {
			t.Errorf("event.Type = %q, veut payment_deleted", e.Type)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("aucun event bus reçu apres delete")
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
	store := payzen.NewMemoryStore()
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
	_ = store.Save(tx)

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
	store := payzen.NewMemoryStore()
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
	store := payzen.NewMemoryStore()
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

// TestSimulatePaymentUnknownOutcomeListsAccepted verifie que l'erreur
// enonce les valeurs acceptees. Sans elles, un integrateur qui envoie
// CAPTURED ou AUTHORIZED (avec un Z la ou PayZen ecrit un S) n'a aucun
// moyen de deviner l'attendu autrement qu'en lisant le code.
func TestSimulatePaymentUnknownOutcomeListsAccepted(t *testing.T) {
	t.Parallel()
	logger := discardLogger()
	store := payzen.NewMemoryStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{HMACKey: "k", Publisher: b})
	handler := NewHandler(Deps{
		Store: store, Queue: queue, Publisher: b, Logger: logger, PayzenHandler: ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	addPayment(t, store, "uuid-o", "order-o", 100)

	body, _ := json.Marshal(SimulatePaymentRequest{Outcome: "CAPTURED"})
	resp, err := http.Post(
		server.URL+"/paysim/api/v1/payments/uuid-o/simulate",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	msg := string(raw)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400", resp.StatusCode)
	}
	if !strings.Contains(msg, "CAPTURED") {
		t.Errorf("le message doit rappeler la valeur refusee : %q", msg)
	}
	for _, want := range []string{"PAID", "AUTHORISED", "UNPAID", "EXPIRED", "ABANDONED"} {
		if !strings.Contains(msg, want) {
			t.Errorf("le message doit lister %q, obtenu : %q", want, msg)
		}
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
	store := payzen.NewMemoryStore()
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

// setupWithSQLite construit un handler API avec un PayzenHandler câblé
// sur SQLiteStore (avec les 3 repos réels : payments, subscriptions,
// payment methods). Utile pour tester les endpoints listing qui
// interrogent directement les repos — le mode mémoire retourne
// toujours vide sur ces endpoints.
func setupWithSQLite(t *testing.T) *httptest.Server {
	t.Helper()
	logger := discardLogger()
	db, err := sqlitepkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	payRepo, err := sqlitepkg.NewPaymentsRepository(db)
	if err != nil {
		t.Fatalf("payments repo: %v", err)
	}
	subsRepo, err := sqlitepkg.NewSubscriptionsRepository(db)
	if err != nil {
		t.Fatalf("subs repo: %v", err)
	}
	methodsRepo, err := sqlitepkg.NewPaymentMethodsRepository(db)
	if err != nil {
		t.Fatalf("methods repo: %v", err)
	}
	pzStore := payzen.NewSQLiteStore(payRepo, subsRepo, methodsRepo)
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	queue.SetPublisher(b)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); wg.Wait() })

	ph := payzen.NewHandler(pzStore, queue, logger, payzen.HandlerConfig{
		HMACKey: "test-hmac", Publisher: b,
	})
	handler := NewHandler(Deps{
		Store:             pzStore,
		PaymentRepo:       payRepo,
		SubscriptionRepo:  subsRepo,
		PaymentMethodRepo: methodsRepo,
		Queue:             queue,
		Publisher:         b,
		Logger:            logger,
		PayzenHandler:     ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// setupWithPayzen construit un handler API avec un PayzenHandler câblé —
// nécessaire pour tester les endpoints qui délèguent à payzen (create
// générique, simulate). Extrait ici pour partager la mécanique entre tests.
func setupWithPayzen(t *testing.T, token string) (*httptest.Server, payzen.Store) {
	t.Helper()
	logger := discardLogger()
	store := payzen.NewMemoryStore()
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
		Token:         token,
		PayzenHandler: ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, store
}

func TestCreatePaymentGenericNominal(t *testing.T) {
	t.Parallel()
	server, store := setupWithPayzen(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen",
		Amount:   1500,
		Currency: "EUR",
		OrderID:  "ORDER-42",
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, veut 201", resp.StatusCode)
	}
	var got CreatePaymentOutput
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UUID == "" {
		t.Errorf("UUID vide")
	}
	if got.Provider != "payzen" {
		t.Errorf("Provider = %q, veut payzen", got.Provider)
	}
	if got.State != "initiated" {
		t.Errorf("State = %q, veut initiated", got.State)
	}
	// La transaction doit être vraiment persistée côté store.
	tx, _ := store.ByUUID(got.UUID)
	if tx == nil {
		t.Fatalf("transaction absente du store apres create")
	}
	if tx.OrderID != "ORDER-42" || tx.Amount != 1500 || tx.Currency != "EUR" {
		t.Errorf("transaction persistée incorrecte: %+v", tx)
	}
}

func TestCreatePaymentGenericDefautPayzen(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	// Provider vide → payzen par défaut, pour ne pas alourdir les
	// scénarios monoprovider.
	body := []byte(`{"amount":1000,"currency":"EUR","orderId":"O-D"}`)
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, veut 201", resp.StatusCode)
	}
	var got CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Provider != "payzen" {
		t.Errorf("Provider = %q, veut payzen (defaut)", got.Provider)
	}
}

func TestCreatePaymentGenericProviderInconnu(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "stripe",
		Amount:   1000,
		Currency: "EUR",
		OrderID:  "O",
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, veut 400", resp.StatusCode)
	}
	msg, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(msg, []byte(`"stripe" inconnu`)) {
		t.Errorf("message = %q, veut contenir '\"stripe\" inconnu'", msg)
	}
}

func TestCreatePaymentGenericInputInvalide(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	cases := []struct {
		name string
		body string
		want string
	}{
		// amount = 0 est valide depuis le fix REGISTER pur ; seul le
		// négatif est rejeté.
		{"amount negatif", `{"amount":-1,"currency":"EUR","orderId":"O"}`, "montant"},
		{"currency vide", `{"amount":1000,"currency":"","orderId":"O"}`, "devise"},
		{"json casse", `{not json`, "invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
				"application/json", strings.NewReader(c.body))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, veut 400", resp.StatusCode)
			}
		})
	}
}

func TestCreatePaymentGenericSansPayzenHandler(t *testing.T) {
	t.Parallel()
	// setup sans PayzenHandler — l'endpoint doit répondre 503 propre
	// plutôt que crasher sur un pointeur nil.
	server, _, _, _ := setup(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Amount: 1000, Currency: "EUR", OrderID: "O",
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, veut 503", resp.StatusCode)
	}
}

// --- Tests 4.4.5 : token pattern + CB stockée -------------------------------

func TestCreatePaymentEnrollment(t *testing.T) {
	t.Parallel()
	server, store := setupWithPayzen(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     1500,
		Currency:   "EUR",
		OrderID:    "ORDER-E",
		FormAction: "REGISTER_PAY",
		Card: &payzen.Card{
			PAN:         "4111111111111111",
			ExpiryMonth: 12,
			ExpiryYear:  2028,
		},
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, veut 201", resp.StatusCode)
	}
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)

	if out.PaymentMethodToken == "" {
		t.Fatalf("PaymentMethodToken vide — l'enrolement n'a pas produit de token")
	}
	// Le PaymentMethod doit exister côté store.
	pm, _ := store.MethodByToken(out.PaymentMethodToken)
	if pm == nil {
		t.Fatalf("PaymentMethod absent du store apres enrolement")
	}
	if pm.Brand != "VISA" || pm.PANMasked != "411111XXXXXX1111" {
		t.Errorf("Brand/PANMasked = %q/%q", pm.Brand, pm.PANMasked)
	}
	// La Transaction doit porter le token pour propagation dans le webhook ultérieur.
	tx, _ := store.ByUUID(out.UUID)
	if tx == nil || tx.PaymentMethodToken != out.PaymentMethodToken {
		t.Errorf("tx.PaymentMethodToken = %q, veut %q", tx.PaymentMethodToken, out.PaymentMethodToken)
	}
}

func TestCreatePaymentCardSansFormActionEnroleQuandMeme(t *testing.T) {
	t.Parallel()
	server, store := setupWithPayzen(t, "")

	// Card fournie sans REGISTER_PAY : Paysim enrôle quand même — le
	// simulateur stocke tout moyen fourni. Le formAction reste une
	// info métadata sur la Transaction, sans effet sur l'enrôlement.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "O",
		Card: &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.PaymentMethodToken == "" {
		t.Fatalf("PaymentMethodToken vide, veut renseigne (enrolement systematique quand Card fournie)")
	}
	pm, _ := store.MethodByToken(out.PaymentMethodToken)
	if pm == nil {
		t.Errorf("PaymentMethod absent du store apres create")
	}
}

func TestSimulateRefusDirectSurCarteExpiree(t *testing.T) {
	t.Parallel()
	server, store := setupWithPayzen(t, "")

	// Carte expirée dans le passé, fournie au 1er paiement. Le PSP
	// réel refuse dès la présentation ; on reproduit ce comportement.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 5000, Currency: "EUR", OrderID: "DIRECT-EXP",
		Card: &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 1, ExpiryYear: 2020},
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = createResp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(createResp.Body).Decode(&out)

	simBody, _ := json.Marshal(SimulatePaymentRequest{
		Outcome:         "PAID",
		Channel:         "ipn",
		NotificationURL: "http://localhost:1/discard",
	})
	simResp, _ := http.Post(server.URL+"/paysim/api/v1/payments/"+out.UUID+"/simulate",
		"application/json", bytes.NewReader(simBody))
	defer func() { _ = simResp.Body.Close() }()

	tx, _ := store.ByUUID(out.UUID)
	if got := string(tx.Payment.State()); got != "declined" {
		t.Errorf("state = %q, veut declined (carte expiree au 1er paiement)", got)
	}
}

func TestSimulateRefusDirectSurCarteRevoquee(t *testing.T) {
	t.Parallel()
	server, store := setupWithPayzen(t, "")

	// Enrolement + révocation avant simulate → refus au 1er paiement.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 5000, Currency: "EUR", OrderID: "DIRECT-REV",
		Card: &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = createResp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(createResp.Body).Decode(&out)

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/payment-methods/"+out.PaymentMethodToken+"/revoke", nil)
	revResp, _ := http.DefaultClient.Do(req)
	_ = revResp.Body.Close()

	simBody, _ := json.Marshal(SimulatePaymentRequest{
		Outcome: "PAID", Channel: "ipn",
		NotificationURL: "http://localhost:1/discard",
	})
	simResp, _ := http.Post(server.URL+"/paysim/api/v1/payments/"+out.UUID+"/simulate",
		"application/json", bytes.NewReader(simBody))
	defer func() { _ = simResp.Body.Close() }()

	tx, _ := store.ByUUID(out.UUID)
	if got := string(tx.Payment.State()); got != "declined" {
		t.Errorf("state = %q, veut declined (carte revoquee au 1er paiement)", got)
	}
}

func TestSimulateRefusDirectSurMagicPAN(t *testing.T) {
	t.Parallel()
	server, store := setupWithPayzen(t, "")

	// Premier paiement avec un PAN magic de refus.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 5000, Currency: "EUR", OrderID: "DIRECT-DECLINE",
		Card: &payzen.Card{PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = createResp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(createResp.Body).Decode(&out)

	// Simulate avec outcome PAID : le magic PAN doit forcer UNPAID.
	simBody, _ := json.Marshal(SimulatePaymentRequest{
		Outcome:   "PAID",
		Channel:   "ipn",
		NotificationURL: "http://localhost:1/discard",
	})
	simResp, _ := http.Post(server.URL+"/paysim/api/v1/payments/"+out.UUID+"/simulate",
		"application/json", bytes.NewReader(simBody))
	defer func() { _ = simResp.Body.Close() }()
	if simResp.StatusCode != http.StatusAccepted {
		t.Fatalf("simulate status = %d, veut 202", simResp.StatusCode)
	}

	// Le paiement doit etre passe en declined via applyOutcome UNPAID.
	tx, _ := store.ByUUID(out.UUID)
	if tx == nil {
		t.Fatalf("tx absente apres simulate")
	}
	if got := string(tx.Payment.State()); got != "declined" {
		t.Errorf("state = %q, veut declined (magic PAN au 1er paiement)", got)
	}
}

func TestChargeTokenNominal(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	// 1er paiement avec enrolement.
	token := enroll(t, server, "4111111111111111", 12, 2028)

	// Rejeu via le token — état captured immédiat.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 2000, Currency: "EUR", OrderID: "REPLAY",
		PaymentMethodToken: token,
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("rejeu status = %d, veut 201", resp.StatusCode)
	}
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.State != "captured" {
		t.Errorf("state = %q, veut captured (rejeu one-click)", out.State)
	}
	if out.PaymentMethodToken != token {
		t.Errorf("token echo = %q, veut %q", out.PaymentMethodToken, token)
	}
}

func TestChargeTokenCarteExpiree(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	// Carte expirée dans le passé : refus immédiat au rejeu.
	token := enroll(t, server, "4111111111111111", 3, 2020)

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "R-EXP",
		PaymentMethodToken: token,
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.State != "declined" {
		t.Errorf("state = %q, veut declined (carte expiree)", out.State)
	}
}

func TestChargeTokenCarteRevoquee(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	token := enroll(t, server, "4111111111111111", 12, 2028)

	// Révocation via l'endpoint dédié.
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/payment-methods/"+token+"/revoke", nil)
	revResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = revResp.Body.Close()
	if revResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, veut 204", revResp.StatusCode)
	}

	// Rejeu → refus.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "R-REV",
		PaymentMethodToken: token,
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.State != "declined" {
		t.Errorf("state = %q, veut declined (carte revoquee)", out.State)
	}
}

func TestChargeTokenMagicDeclinedPAN(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	// PAN Visa réservé pour refus systématique.
	token := enroll(t, server, "4000000000000002", 12, 2028)

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "R-MAGIC",
		PaymentMethodToken: token,
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.State != "declined" {
		t.Errorf("state = %q, veut declined (PAN magic de refus)", out.State)
	}
}

func TestChargeTokenInconnu(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "R-INC",
		PaymentMethodToken: "does-not-exist",
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400 (token inconnu)", resp.StatusCode)
	}
}

func TestRevokePaymentMethodInconnuRetourne204(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/payment-methods/does-not-exist/revoke", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, veut 204 (idempotent sur token inconnu)", resp.StatusCode)
	}
}

// enroll est un helper : crée un paiement avec Card + REGISTER_PAY et
// retourne le paymentMethodToken produit par Paysim.
func enroll(t *testing.T, server *httptest.Server, pan string, expM, expY int) string {
	t.Helper()
	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     1000,
		Currency:   "EUR",
		OrderID:    "ENROLL",
		FormAction: "REGISTER_PAY",
		Card:       &payzen.Card{PAN: pan, ExpiryMonth: expM, ExpiryYear: expY},
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("enroll POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enroll status = %d, veut 201", resp.StatusCode)
	}
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.PaymentMethodToken == "" {
		t.Fatalf("enroll: PaymentMethodToken vide")
	}
	return out.PaymentMethodToken
}

// --- Tests 7a : endpoints listing subscriptions + payment methods ---------

func TestListPaymentMethods_emptyInMemoryMode(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")
	// setupWithPayzen ne branche pas PaymentMethodRepo → endpoint retourne [].
	resp, err := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", resp.StatusCode)
	}
	var out []PaymentMethodOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 0 {
		t.Errorf("liste = %d entries, veut 0 en mode memoire", len(out))
	}
}

func TestListPaymentMethods_afterEnrollWithSQLite(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	// Enroll une CB.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     1000, Currency: "EUR", OrderID: "O",
		FormAction: "REGISTER_PAY",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	_ = createResp.Body.Close()

	// Liste → doit contenir 1 entrée avec PAN masqué.
	listResp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", listResp.StatusCode)
	}
	var out []PaymentMethodOutput
	_ = json.NewDecoder(listResp.Body).Decode(&out)
	if len(out) != 1 {
		t.Fatalf("liste = %d entries, veut 1", len(out))
	}
	if out[0].PANMasked != "411111XXXXXX1111" {
		t.Errorf("PANMasked = %q, veut 411111XXXXXX1111", out[0].PANMasked)
	}
	if out[0].Brand != "VISA" || out[0].Provider != "payzen" || out[0].Revoked {
		t.Errorf("Brand/Provider/Revoked = %q/%q/%v", out[0].Brand, out[0].Provider, out[0].Revoked)
	}
}

// TestPaymentMethods_listeEtDetailConcordent verrouille la divergence
// qui avait cours : les attributs de carte n'existaient que sur le
// detail, si bien qu'un meme moyen portait un porteur ou pas selon la
// route interrogee. On compare les deux vues plutot que d'asserter des
// valeurs en dur — ainsi le test protege aussi les champs a venir.
func TestPaymentMethods_listeEtDetailConcordent(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen",
		Amount:   1000, Currency: "EUR", OrderID: "O",
		Card: &payzen.Card{
			PAN: "4111111111111111", ExpiryMonth: 8, ExpiryYear: 2029,
			HolderName: "DUPONT JEAN-EMILLE", Country: "US",
			ProductCategory: "DEBIT", IssuerName: "BANQUE DE TEST",
		},
	})
	createResp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = createResp.Body.Close()

	listResp, err := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listResp.Body.Close() }()
	var list []PaymentMethodOutput
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("liste = %d entrees, veut 1", len(list))
	}

	detailResp, err := http.Get(server.URL + "/paysim/api/v1/payment-methods/" + list[0].Token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = detailResp.Body.Close() }()
	var detail PaymentMethodOutput
	_ = json.NewDecoder(detailResp.Body).Decode(&detail)

	if list[0] != detail {
		t.Errorf("liste et detail divergent : liste=%+v detail=%+v", list[0], detail)
	}
	// Garde-fou explicite : sans lui, deux vues vides concorderaient.
	if detail.HolderName != "DUPONT JEAN-EMILLE" {
		t.Errorf("HolderName = %q, veut DUPONT JEAN-EMILLE", detail.HolderName)
	}
}

// TestPaymentMethods_verdictExploitabilite : une carte que tout debit
// refusera ne doit pas etre indistinguable d'une carte valide dans la
// collection. Le verdict est derive a la lecture, donc il couvre les
// trois causes sans qu'aucune ne soit persistee.
func TestPaymentMethods_verdictExploitabilite(t *testing.T) {
	t.Parallel()
	cases := []struct {
		nom        string
		card       payzen.Card
		wantUsable bool
		wantReason string
	}{
		{"carte valide", payzen.Card{
			PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030,
		}, true, ""},
		{"PAN de refus", payzen.Card{
			PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2030,
		}, false, "carte de test refusee"},
		{"carte expiree", payzen.Card{
			PAN: "4111111111111111", ExpiryMonth: 1, ExpiryYear: 2020,
		}, false, "moyen de paiement expire"},
	}
	for _, c := range cases {
		t.Run(c.nom, func(t *testing.T) {
			t.Parallel()
			server := setupWithSQLite(t)
			body, _ := json.Marshal(CreatePaymentInput{
				Provider: "payzen", Amount: 1000, Currency: "EUR",
				OrderID: "O", Card: &c.card,
			})
			resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
				"application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()

			listResp, err := http.Get(server.URL + "/paysim/api/v1/payment-methods")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listResp.Body.Close() }()
			var list []PaymentMethodOutput
			_ = json.NewDecoder(listResp.Body).Decode(&list)
			if len(list) != 1 {
				t.Fatalf("liste = %d entrees, veut 1", len(list))
			}
			if list[0].Usable != c.wantUsable {
				t.Errorf("Usable = %v, veut %v", list[0].Usable, c.wantUsable)
			}
			if list[0].UnusableReason != c.wantReason {
				t.Errorf("UnusableReason = %q, veut %q", list[0].UnusableReason, c.wantReason)
			}
			// Le moyen reste enregistre quoi qu'il arrive : c'est ce qui
			// permet de rejouer un impaye sur une carte de refus.
			if list[0].Token == "" {
				t.Error("le moyen doit rester enregistre meme inexploitable")
			}
		})
	}
}

func TestListPaymentMethods_afterRevoke(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	// Enroll puis revoke → la liste doit refléter revoked=true.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     1000, Currency: "EUR", OrderID: "O",
		FormAction: "REGISTER_PAY",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var created CreatePaymentOutput
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	_ = createResp.Body.Close()

	revReq, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/payment-methods/"+created.PaymentMethodToken+"/revoke", nil)
	rr, _ := http.DefaultClient.Do(revReq)
	_ = rr.Body.Close()

	listResp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	defer func() { _ = listResp.Body.Close() }()
	var out []PaymentMethodOutput
	_ = json.NewDecoder(listResp.Body).Decode(&out)
	if len(out) != 1 || !out[0].Revoked {
		t.Errorf("liste = %+v, veut 1 entree avec Revoked=true", out)
	}
}

func TestListSubscriptions_afterCreateWithSQLite(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	// Enroll une CB puis crée un abonnement.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     100, Currency: "EUR", OrderID: "INIT",
		FormAction: "REGISTER_PAY",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var created CreatePaymentOutput
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	_ = createResp.Body.Close()

	subBody, _ := json.Marshal(CreateSubscriptionInput{
		PaymentMethodToken: created.PaymentMethodToken,
		Amount: 2990, Currency: "EUR", OrderID: "SUB",
		EffectDate: "2026-09-01", Rrule: "RRULE:FREQ=MONTHLY",
	})
	subResp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(subBody))
	_ = subResp.Body.Close()

	// Liste des subscriptions → doit contenir 1 entrée.
	listResp, _ := http.Get(server.URL + "/paysim/api/v1/subscriptions")
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", listResp.StatusCode)
	}
	var out []SubscriptionOutput
	_ = json.NewDecoder(listResp.Body).Decode(&out)
	if len(out) != 1 {
		t.Fatalf("liste = %d subs, veut 1", len(out))
	}
	if out[0].Amount != 2990 || out[0].PaymentMethodToken != created.PaymentMethodToken {
		t.Errorf("sub = %+v, veut amount 2990 + bon token", out[0])
	}
}

func TestGetPaymentMethod_afterEnroll(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	// Enroll une CB.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     1000, Currency: "EUR", OrderID: "O",
		FormAction: "REGISTER_PAY",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var created CreatePaymentOutput
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	_ = createResp.Body.Close()

	getResp, err := http.Get(server.URL + "/paysim/api/v1/payment-methods/" + created.PaymentMethodToken)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", getResp.StatusCode)
	}
	var out PaymentMethodOutput
	_ = json.NewDecoder(getResp.Body).Decode(&out)
	if out.Token != created.PaymentMethodToken {
		t.Errorf("Token = %q, veut %q", out.Token, created.PaymentMethodToken)
	}
	if out.Brand != "VISA" || out.PANMasked != "411111XXXXXX1111" {
		t.Errorf("Brand/PANMasked = %q/%q", out.Brand, out.PANMasked)
	}
}

func TestGetPaymentMethod_unknown(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)
	resp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods/does-not-exist")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, veut 404", resp.StatusCode)
	}
}

func TestGetPaymentMethod_inMemoryMode(t *testing.T) {
	t.Parallel()
	// setupWithPayzen ne branche pas PaymentMethodRepo → 404.
	server, _ := setupWithPayzen(t, "")
	resp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods/any-token")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, veut 404 (mode memoire sans repo)", resp.StatusCode)
	}
}

// TestCreatePayment_refusNeRendPasDeToken : annoncer un alias dans la
// meme reponse qu'un refus laisse croire a un moyen debitable. Le moyen
// reste enregistre — la collection l'expose avec son verdict — mais la
// reponse de creation n'en fait pas etat.
func TestCreatePayment_refusNeRendPasDeToken(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "REFUS",
		Card: &payzen.Card{PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)

	// Sans autoplay le paiement reste initiated : le token est alors
	// legitime, rien n'a encore ete refuse.
	if out.State == "declined" && out.PaymentMethodToken != "" {
		t.Errorf("un paiement refuse ne doit pas rendre de token, obtenu %q", out.PaymentMethodToken)
	}
	// Le moyen est bien enregistre malgre tout.
	listResp, err := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listResp.Body.Close() }()
	var list []PaymentMethodOutput
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	if len(list) != 1 || list[0].Usable {
		t.Errorf("le moyen doit exister et etre marque inexploitable : %+v", list)
	}
}
