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
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/providers/payzen"
	"github.com/sprimault/paysim/internal/store/inmem"
	sqlitepkg "github.com/sprimault/paysim/internal/store/sqlite"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newMemStore monte un Store adosse a des depots en memoire, tel que
// cmd/paysim le fait pour PAYSIM_STORE=memory. Remplace le
// payzen.MemoryStore supprime : le mode memoire ne passe plus par une
// implementation distincte du contrat.
func newMemStore() payzen.Store {
	return payzen.NewRepoStore(
		inmem.NewPaymentsRepository(0, nil),
		inmem.NewSubscriptionsRepository(),
		inmem.NewPaymentMethodsRepository(),
	)
}

// setup construit un environnement complet : store peuplé + queue +
// bus + handler API branché.
func setup(t *testing.T, token string) (*httptest.Server, payzen.Store, *delivery.Queue, *bus.Bus) {
	t.Helper()
	logger := discardLogger()
	store := newMemStore()
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

// TestGetPaymentCompteSesLivraisons couvre un defaut qui ne se voyait
// qu'en comparant les deux endpoints : le detail renvoyait toujours
// webhookCount 0, alors que la liste comptait juste. PaymentDetail
// embarque PaymentSummary, donc le champ partait quand meme — un zero
// n'annoncait pas « non compte », il affirmait « aucune livraison ».
//
// L'assertion porte sur les deux endpoints a la fois : c'est leur
// divergence qui est le bogue, pas la valeur prise isolement.
func TestGetPaymentCompteSesLivraisons(t *testing.T) {
	t.Parallel()
	aval := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer aval.Close()

	server, store, queue, _ := setup(t, "")
	addPayment(t, store, "uuid-compte", "order-compte", 1500)
	for _, w := range []struct {
		id     string
		replay bool
	}{{"wh-1", false}, {"wh-2", true}, {"wh-3", true}} {
		_ = queue.Enqueue(delivery.Webhook{
			ID:          w.id,
			URL:         aval.URL,
			Body:        []byte(`{"x":1}`),
			Headers:     map[string]string{"Content-Type": "application/json"},
			PaymentUUID: "uuid-compte",
			Replay:      w.replay,
		})
	}

	detail := func() PaymentDetail {
		t.Helper()
		resp, err := http.Get(server.URL + "/paysim/api/v1/payments/uuid-compte")
		if err != nil {
			t.Fatalf("GET detail : %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out PaymentDetail
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// Les trois livraisons partent en file : on attend qu'elles soient
	// historisees plutot que de supposer un delai.
	deadline := time.Now().Add(2 * time.Second)
	var got PaymentDetail
	for time.Now().Before(deadline) {
		got = detail()
		if got.WebhookCount == 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got.WebhookCount != 3 {
		t.Fatalf("detail webhookCount = %d, veut 3", got.WebhookCount)
	}
	if got.WebhookReplayCount != 2 {
		t.Errorf("detail webhookReplayCount = %d, veut 2", got.WebhookReplayCount)
	}

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments")
	defer func() { _ = resp.Body.Close() }()
	var liste []PaymentSummary
	_ = json.NewDecoder(resp.Body).Decode(&liste)
	if len(liste) != 1 {
		t.Fatalf("liste = %d paiements, veut 1", len(liste))
	}
	// La comparaison porte sur le sommaire entier, pas sur les deux
	// compteurs : c'est l'invariant qui a cede, et il attrape toute
	// divergence future entre les deux vues du meme paiement — un champ
	// ajoute a l'une et oublie dans l'autre, comme les compteurs l'ont
	// ete, ou comme les moyens de paiement avant eux.
	if !reflect.DeepEqual(liste[0], got.PaymentSummary) {
		t.Errorf("le sommaire diverge entre les deux vues :\n  liste  = %+v\n  detail = %+v",
			liste[0], got.PaymentSummary)
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

// TestListWebhooksFiltreParPaiement couvre le defaut qui faisait
// afficher, dans le detail d'un paiement, le kr-answer d'un autre :
// sans rattachement en base, l'UI prenait la tete de la liste globale.
func TestListWebhooksFiltreParPaiement(t *testing.T) {
	t.Parallel()
	aval := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer aval.Close()

	server, _, queue, _ := setup(t, "")
	for _, w := range []struct{ id, payment string }{
		{"wh-a", "pay-a"},
		{"wh-b", "pay-b"},
	} {
		_ = queue.Enqueue(delivery.Webhook{
			ID:          w.id,
			URL:         aval.URL,
			Body:        []byte(`{"x":1}`),
			Headers:     map[string]string{"Content-Type": "application/json"},
			PaymentUUID: w.payment,
		})
	}

	list := func(query string) []WebhookEntry {
		t.Helper()
		resp, err := http.Get(server.URL + "/paysim/api/v1/webhooks" + query)
		if err != nil {
			t.Fatalf("GET %s: %v", query, err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out []WebhookEntry
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// Attendre que les deux livraisons soient historisees.
	deadline := time.Now().Add(2 * time.Second)
	var all []WebhookEntry
	for time.Now().Before(deadline) {
		all = list("")
		if len(all) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(all) != 2 {
		t.Fatalf("sans filtre = %d entrees, veut 2", len(all))
	}

	ofA := list("?paymentUuid=pay-a")
	if len(ofA) != 1 {
		t.Fatalf("filtre pay-a = %d entrees, veut 1", len(ofA))
	}
	if ofA[0].ID != "wh-a" {
		t.Errorf("filtre pay-a = %s, veut wh-a", ofA[0].ID)
	}
	if ofA[0].PaymentUUID != "pay-a" {
		t.Errorf("PaymentUUID = %q, veut pay-a — le champ n'est pas expose", ofA[0].PaymentUUID)
	}

	// Un uuid sans livraison doit rendre une liste vide, pas retomber
	// sur la liste complete : c'est cette confusion qui produisait le
	// mauvais payload.
	if none := list("?paymentUuid=pay-inconnu"); len(none) != 0 {
		t.Errorf("uuid inconnu = %d entrees, veut 0 — le filtre est ignore", len(none))
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

	// 4. Rejouer le rejeu ne fait pas grossir l'identifiant. Provoquer
	// des livraisons en double est la raison d'être du simulateur : le
	// geste se répète, et chaque répétition ajoutait un préfixe.
	//
	// Même race qu'en 1 : le compteur du serveur aval bouge avant que
	// l'historique ne porte l'entrée, et c'est lui qu'interroge
	// /replay.
	deadline = time.Now().Add(2 * time.Second)
	found = false
	for time.Now().Before(deadline) && !found {
		for _, r := range queue.Recent(50) {
			if r.Webhook.ID == body.NewDeliveryID {
				found = true
				break
			}
		}
		if !found {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !found {
		t.Fatalf("rejeu %q absent de l'historique", body.NewDeliveryID)
	}

	req, _ = http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/webhooks/"+body.NewDeliveryID+"/replay", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("replay du replay status = %d, veut 202", resp2.StatusCode)
	}
	var second ReplayWebhookResponse
	_ = json.NewDecoder(resp2.Body).Decode(&second)
	if !strings.HasPrefix(second.NewDeliveryID, "replay-wh-original-") {
		t.Errorf("NewDeliveryID = %q, doit repartir de la livraison d'origine", second.NewDeliveryID)
	}
	if len(second.NewDeliveryID) != len(body.NewDeliveryID) {
		t.Errorf("identifiant de %d caracteres apres deux rejeux, %d apres un seul : il s'empile",
			len(second.NewDeliveryID), len(body.NewDeliveryID))
	}
}

func TestRacineLivraison(t *testing.T) {
	t.Parallel()
	cas := []struct {
		nom, in, veut string
	}{
		{"livraison d'origine", "wh-42", "wh-42"},
		{"rejeu simple", "replay-wh-42-144028.486128", "wh-42"},
		{"UUID a tirets", "replay-90252b82-ce2d-447f-b0e4-84c4987dab53-144028.486128",
			"90252b82-ce2d-447f-b0e4-84c4987dab53"},
		// Identifiants deja empiles sur une instance en cours : ils
		// cessent de croitre, sans qu'on cherche a les demeler.
		{"rejeu deja empile", "replay-replay-wh-42-144028.486128-144141.683509",
			"replay-wh-42-144028.486128"},
		// Rien a couper : mieux vaut rendre l'entree telle quelle qu'un
		// identifiant vide, qui serait introuvable a la relecture.
		{"prefixe seul", "replay-", "replay-"},
		{"sans horodatage", "replay-wh42", "replay-wh42"},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			if got := racineLivraison(c.in); got != c.veut {
				t.Errorf("racineLivraison(%q) = %q, veut %q", c.in, got, c.veut)
			}
		})
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
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	queue.SetPublisher(b)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); wg.Wait() })

	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{
		HMACKey:   "test-hmac", RESTPassword: "pwd-rest",
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
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{HMACKey: "k", RESTPassword: "pwd-rest", Publisher: b})
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
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{HMACKey: "k", RESTPassword: "pwd-rest", Publisher: b})
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
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{HMACKey: "k", RESTPassword: "pwd-rest", Publisher: b})
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
	store := newMemStore()
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
// sur RepoStore (avec les 3 repos réels : payments, subscriptions,
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
	pzStore := payzen.NewRepoStore(payRepo, subsRepo, methodsRepo)
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	queue.SetPublisher(b)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); wg.Wait() })

	ph := payzen.NewHandler(pzStore, queue, logger, payzen.HandlerConfig{
		HMACKey: "test-hmac", RESTPassword: "pwd-rest", Publisher: b,
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
// setupWithRepos monte l'API comme le fait cmd/paysim : store et depots
// construits ensemble, et les depots passes au handler.
//
// Distinct de setupWithPayzen, qui n'en cable aucun — celui-la sert aux
// tests qui verifient justement le 501 d'un depot absent. Les endpoints
// qui lisent un depot ont besoin de ce montage-ci, faute de quoi ils
// testeraient une configuration que la production n'emprunte pas.
func setupWithRepos(t *testing.T, token string) (*httptest.Server, payzen.Store) {
	t.Helper()
	logger := discardLogger()
	paymentRepo := inmem.NewPaymentsRepository(0, nil)
	subsRepo := inmem.NewSubscriptionsRepository()
	methodsRepo := inmem.NewPaymentMethodsRepository()
	store := payzen.NewRepoStore(paymentRepo, subsRepo, methodsRepo)

	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	queue.SetPublisher(b)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); wg.Wait() })

	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{
		HMACKey:   "test-hmac", RESTPassword: "pwd-rest",
		Publisher: b,
	})
	handler := NewHandler(Deps{
		Store:             store,
		PaymentRepo:       paymentRepo,
		SubscriptionRepo:  subsRepo,
		PaymentMethodRepo: methodsRepo,
		Queue:             queue,
		Publisher:         b,
		Logger:            logger,
		Token:             token,
		PayzenHandler:     ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, store
}

// setupWithPayzen monte l'API sans aucun depot cable.
//
// Conserve pour les tests qui verifient le refus explicite dans cette
// configuration ; tout le reste doit passer par setupWithRepos.
func setupWithPayzen(t *testing.T, token string) (*httptest.Server, payzen.Store) {
	t.Helper()
	logger := discardLogger()
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)
	b := bus.New()
	queue.SetPublisher(b)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); wg.Wait() })

	ph := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{
		HMACKey:   "test-hmac", RESTPassword: "pwd-rest",
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

	// Rien encore : l'autorisation n'a pas eu lieu, donc pas d'alias.
	// « L'alias (token) ne sera pas cree si la demande d'autorisation ou
	// de renseignement est refusee » — et tant qu'elle n'a pas eu lieu,
	// il n'y a rien a annoncer non plus.
	if out.PaymentMethodToken != "" {
		t.Errorf("token annonce des la creation (%q) : l'autorisation n'a pas encore eu lieu",
			out.PaymentMethodToken)
	}
	if tx, _ := store.ByUUID(out.UUID); tx != nil && tx.PaymentMethodToken != "" {
		t.Errorf("la transaction porte deja un token (%q) avant le paiement",
			tx.PaymentMethodToken)
	}

	// Le porteur paie : l'alias nait maintenant.
	simulerPaiement(t, server, out.UUID, payzen.OutcomePaid)

	tx, _ := store.ByUUID(out.UUID)
	if tx == nil || tx.PaymentMethodToken == "" {
		t.Fatal("aucun token apres un paiement accepte")
	}
	pm, _ := store.MethodByToken(tx.PaymentMethodToken)
	if pm == nil {
		t.Fatalf("PaymentMethod absent du store apres enrolement")
	}
	if pm.Brand != "VISA" || pm.PANMasked != "411111XXXXXX1111" {
		t.Errorf("Brand/PANMasked = %q/%q", pm.Brand, pm.PANMasked)
	}
}

// simulerPaiement joue l'acte du porteur sur un paiement en attente.
func simulerPaiement(t *testing.T, server *httptest.Server, uuid, outcome string) {
	t.Helper()
	body, _ := json.Marshal(SimulatePaymentRequest{
		Channel: "ipn", Outcome: outcome,
		NotificationURL: "http://127.0.0.1:1/sink",
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments/"+uuid+"/simulate",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	// 202 : la livraison est enfilee, la transition est deja faite.
	if resp.StatusCode != http.StatusAccepted {
		corps, _ := io.ReadAll(resp.Body)
		t.Fatalf("simulate status = %d : %s", resp.StatusCode, corps)
	}
}

func TestCreatePaymentCardSansFormActionEnroleQuandMeme(t *testing.T) {
	t.Parallel()
	server, store := setupWithPayzen(t, "")

	// Card fournie sans formAction, avec un montant a debiter : c'est le
	// montant qui tranche, pas l'etiquette. L'alias attend donc l'issue
	// du paiement, comme n'importe quel REGISTER_PAY.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "O",
		Card: &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.PaymentMethodToken != "" {
		t.Fatalf("token annonce avant tout paiement : %q", out.PaymentMethodToken)
	}

	simulerPaiement(t, server, out.UUID, payzen.OutcomePaid)

	tx, _ := store.ByUUID(out.UUID)
	if tx == nil || tx.PaymentMethodToken == "" {
		t.Fatal("aucun alias apres un paiement accepte, malgre une carte fournie")
	}
	if pm, _ := store.MethodByToken(tx.PaymentMethodToken); pm == nil {
		t.Errorf("PaymentMethod absent du store apres enrolement")
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

	// Une révocation suppose un alias : on enrôle d'abord, on révoque,
	// puis on présente ce moyen à un paiement. C'est l'ordre réel — on
	// ne révoque pas une carte qui n'a jamais été enregistrée.
	token := enroll(t, server, "4111111111111111", 12, 2028)

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/payment-methods/"+token+"/revoke", nil)
	revResp, _ := http.DefaultClient.Do(req)
	_ = revResp.Body.Close()

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 5000, Currency: "EUR", OrderID: "DIRECT-REV",
		PaymentMethodToken: token,
	})
	createResp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = createResp.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(createResp.Body).Decode(&out)

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

	// Une carte ne s'enrôle jamais déjà expirée — PayZen refuserait
	// l'autorisation et ne créerait pas d'alias. Le cas réel est un
	// alias valide que le temps a rattrapé : on l'enrôle sain, puis on
	// le périme.
	token := enroll(t, server, "4111111111111111", 12, 2030)
	expireMethod(t, server, token)

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
// expireMethod fait vieillir un alias jusqu'a le perimer, via l'action
// de controle prevue pour ca.
func expireMethod(t *testing.T, server *httptest.Server, token string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api/v1/payment-methods/"+token+"/expire", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expire status = %d, veut 204", resp.StatusCode)
	}
}

func enroll(t *testing.T, server *httptest.Server, pan string, expM, expY int) string {
	t.Helper()
	// REGISTER pur : l'alias est ce qu'on veut, pas un paiement. Un
	// REGISTER_PAY resterait suspendu au parcours du porteur et ne
	// rendrait aucun token avant la simulation — comme chez PayZen.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     0,
		Currency:   "EUR",
		OrderID:    "ENROLL",
		FormAction: "REGISTER",
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

// TestListPaymentMethods_sansDepot : un handler monte sans depot ne
// repond plus 200 avec une liste vide.
//
// Ce test verifiait l'inverse, et son nom disait « mode memoire » alors
// qu'il decrivait un defaut de cablage. La confusion a coute cher : le
// mode memoire cablait effectivement zero depot, l'API affirmait donc
// qu'aucun alias n'existait, et ce test figeait ce mensonge en
// comportement attendu. Les deux backends cablent desormais leurs
// depots ; un handler qui n'en a pas est une erreur de montage.
func TestListPaymentMethods_sansDepot(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")
	resp, err := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, veut 501 — une liste vide ferait croire qu'aucun alias n'existe",
			resp.StatusCode)
	}
}

func TestListPaymentMethods_afterEnrollWithSQLite(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	// Enroll une CB.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider:   "payzen",
		Amount:     0, Currency: "EUR", OrderID: "O",
		FormAction: "REGISTER",
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
		Amount:   0, Currency: "EUR", OrderID: "O",
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
		// Une carte ne s'enrole jamais deja expiree : on l'enregistre
		// saine, puis on la fait vieillir — le cas reel.
		{"carte expiree", payzen.Card{
			PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030,
		}, false, "moyen de paiement expire"},
	}
	for _, c := range cases {
		t.Run(c.nom, func(t *testing.T) {
			t.Parallel()
			server := setupWithSQLite(t)
			body, _ := json.Marshal(CreatePaymentInput{
				Provider: "payzen", Amount: 0, Currency: "EUR",
				FormAction: "REGISTER", OrderID: "O", Card: &c.card,
			})
			resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
				"application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			var cree CreatePaymentOutput
			_ = json.NewDecoder(resp.Body).Decode(&cree)
			_ = resp.Body.Close()
			if c.wantReason == "moyen de paiement expire" {
				expireMethod(t, server, cree.PaymentMethodToken)
			}

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
		Amount:     0, Currency: "EUR", OrderID: "O",
		FormAction: "REGISTER",
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
		Amount:     0, Currency: "EUR", OrderID: "INIT",
		FormAction: "REGISTER",
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
		Amount:     0, Currency: "EUR", OrderID: "O",
		FormAction: "REGISTER",
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

// TestGetPaymentMethod_sansDepot : sans depot, le detail ne repond plus
// 404. Un 404 affirme « ce token n'existe pas » ; le serveur ne sait en
// realite pas repondre, et l'integrateur en concluait a tort que son
// enrolement avait echoue alors que le token etait debitable.
func TestGetPaymentMethod_sansDepot(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")
	resp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods/any-token")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, veut 501 — un 404 ferait conclure a un enrolement echoue",
			resp.StatusCode)
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
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "REFUS",
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

// TestReset_videToutesLesTables : une reinitialisation doit vider les
// quatre collections et rendre compte de ce qu'elle a supprime — c'est
// ce compte qui permet a l'interface d'annoncer ce qui va disparaitre
// plutot qu'un « etes-vous sur ? » qui n'informe de rien.
func TestReset_videToutesLesTables(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	// Un enrolement produit un paiement et un moyen ; on y ajoute un
	// abonnement pour couvrir les trois collections persistees.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "O",
		Card: &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()

	subBody, _ := json.Marshal(CreateSubscriptionInput{
		Provider: "payzen", PaymentMethodToken: created.PaymentMethodToken,
		Amount: 990, Currency: "EUR", OrderID: "SUB",
	})
	subResp, err := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(subBody))
	if err != nil {
		t.Fatal(err)
	}
	_ = subResp.Body.Close()

	// Réinitialisation.
	rst, err := http.Post(server.URL+"/paysim/api/v1/reset", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rst.Body.Close() }()
	if rst.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", rst.StatusCode)
	}
	var out ResetOutput
	_ = json.NewDecoder(rst.Body).Decode(&out)

	if out.Payments < 1 {
		t.Errorf("Payments = %d, veut au moins 1", out.Payments)
	}
	if out.PaymentMethods < 1 {
		t.Errorf("PaymentMethods = %d, veut au moins 1", out.PaymentMethods)
	}
	if out.Subscriptions < 1 {
		t.Errorf("Subscriptions = %d, veut au moins 1", out.Subscriptions)
	}

	// Les collections doivent être vides ensuite.
	for _, path := range []string{"payments", "payment-methods", "subscriptions"} {
		r, err := http.Get(server.URL + "/paysim/api/v1/" + path)
		if err != nil {
			t.Fatal(err)
		}
		var items []json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&items)
		_ = r.Body.Close()
		if len(items) != 0 {
			t.Errorf("%s : %d entrees restantes apres reset", path, len(items))
		}
	}
}

// TestReset_baseVideNEchouePas : reinitialiser deux fois de suite doit
// etre sans effet la seconde, pas une erreur.
func TestReset_baseVideNEchouePas(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)
	for i := range 2 {
		resp, err := http.Post(server.URL+"/paysim/api/v1/reset", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("passe %d : status = %d, veut 200", i+1, resp.StatusCode)
		}
	}
}

// La réponse de création doit porter la marque à côté du token : sans
// elle, le marchand enregistre le moyen avec sa valeur par défaut, et
// l'IPN suivant ne la corrige pas — il tombe dans la branche « déjà
// enregistré » et n'écrit rien. La marque erronée survivrait donc
// jusqu'au paiement récurrent suivant.
func TestCreatePaymentRetourneLaMarqueAvecLeToken(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "BRAND-MC",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "5555555555554444", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()

	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.PaymentMethodToken == "" {
		t.Fatal("PaymentMethodToken vide, veut renseigne")
	}
	if out.Brand != "MASTERCARD" {
		t.Errorf("Brand = %q, veut MASTERCARD", out.Brand)
	}
}

// Sans enrôlement, pas de token, donc rien à qualifier : annoncer une
// marque seule laisserait croire qu'un moyen a été enregistré.
func TestCreatePaymentSansEnrolementNAnnoncePasDeMarque(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "NO-CARD",
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()

	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Brand != "" {
		t.Errorf("Brand = %q, veut vide (aucun moyen enrole)", out.Brand)
	}
}

// Sur un refus, le token est retiré de la réponse — la marque doit
// suivre. En laisser une sans alias décrirait un moyen que la réponse
// se refuse justement à annoncer.
func TestCreatePaymentRefuseNAnnonceNiTokenNiMarque(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	// Enrôlement d'un PAN de test réservé au refus systématique. Celui
	// de la provision insuffisante, le seul qui s'enrôle : la
	// vérification n'engage aucun montant, donc n'interroge pas le
	// solde. Les motifs tenant au statut de la carte échouent dès
	// l'enrôlement et ne donneraient aucun alias à rejouer.
	enrol, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "ENROL-KO",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	r1, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(enrol))
	var enrolled CreatePaymentOutput
	_ = json.NewDecoder(r1.Body).Decode(&enrolled)
	_ = r1.Body.Close()
	if enrolled.PaymentMethodToken == "" {
		t.Fatal("enrolement sans token")
	}

	// Rejeu one-click sur ce moyen : refus immédiat.
	replay, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 2000, Currency: "EUR", OrderID: "REPLAY-KO",
		PaymentMethodToken: enrolled.PaymentMethodToken,
	})
	r2, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(replay))
	defer func() { _ = r2.Body.Close() }()

	var out CreatePaymentOutput
	_ = json.NewDecoder(r2.Body).Decode(&out)
	if out.State != "declined" {
		t.Fatalf("state = %q, veut declined", out.State)
	}
	if out.PaymentMethodToken != "" {
		t.Errorf("PaymentMethodToken = %q, veut vide sur un refus", out.PaymentMethodToken)
	}
	if out.Brand != "" {
		t.Errorf("Brand = %q, veut vide sur un refus", out.Brand)
	}
}

// Le contexte marchand doit traverser sans perte : c'est ce qui a
// manqué deux fois, sur customer.reference puis sur les blocs shipping
// et extra. Un champ non modélisé disparaît au décodage JSON, sans
// erreur ni trace — le test compare donc ce qui ressort à ce qui entre.
func TestCreatePaymentPropageCustomerComplet(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	envoye := payzen.Customer{
		Email:     "alice@example.com",
		Reference: "demo-org",
		BillingDetails: payzen.BillingDetails{
			FirstName: "Alice", LastName: "MARTIN",
			Address: "1 rue de la Paix", City: "Paris",
			ZipCode: "75002", Country: "FR",
		},
		ShippingDetails: payzen.ShippingDetails{
			Category: "COMPANY", LegalName: "ACME SARL", IdentityCode: "12345678900011",
			FirstName: "Bob", LastName: "DURAND", PhoneNumber: "+33600000000",
			StreetNumber: "12", Address: "avenue des Champs", Address2: "batiment C",
			District: "8e", ZipCode: "75008", City: "Paris", State: "IDF", Country: "FR",
			DeliveryCompanyName: "TRANSPORTEUR X",
			ShippingSpeed:       "EXPRESS",
			ShippingMethod:      "RELAY_POINT",
		},
		ExtraDetails: payzen.ExtraDetails{
			IPAddress: "203.0.113.7", FingerPrintID: "fp-abc123",
			BrowserUserAgent: "Mozilla/5.0", BrowserAccept: "text/html",
		},
	}

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "CTX-1",
		Customer: envoye,
		Metadata: map[string]string{"plan": "pro"},
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()

	detResp, err := http.Get(server.URL + "/paysim/api/v1/payments/" + out.UUID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = detResp.Body.Close() }()
	var det PaymentDetail
	if err := json.NewDecoder(detResp.Body).Decode(&det); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(det.Customer, envoye) {
		t.Errorf("customer restitue different de l'envoye\n obtenu : %+v\n veut   : %+v",
			det.Customer, envoye)
	}
	if det.Metadata["plan"] != "pro" {
		t.Errorf("metadata = %v, veut plan=pro", det.Metadata)
	}
}

// Même vérification en SQLite : le customer y transite par une colonne
// JSON désérialisée en bloc, donc aucune migration n'est requise pour
// de nouveaux champs. Ce test verrouille cette propriété — un décodage
// champ par champ introduit plus tard les perdrait en silence.
func TestCreatePaymentPropageCustomerCompletSQLite(t *testing.T) {
	t.Parallel()
	server := setupWithSQLite(t)

	envoye := payzen.Customer{
		Email:     "bob@example.com",
		Reference: "org-42",
		ShippingDetails: payzen.ShippingDetails{
			Category: "PRIVATE", FirstName: "Bob", LastName: "DURAND",
			StreetNumber: "5", Address: "rue Neuve", ZipCode: "69001",
			City: "Lyon", Country: "FR",
			ShippingSpeed: "PRIORITY", ShippingMethod: "DIGITAL_GOOD",
		},
		ExtraDetails: payzen.ExtraDetails{
			IPAddress: "198.51.100.4", FingerPrintID: "fp-xyz",
		},
	}

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 2500, Currency: "EUR", OrderID: "CTX-SQL",
		Customer: envoye,
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var out CreatePaymentOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()

	detResp, err := http.Get(server.URL + "/paysim/api/v1/payments/" + out.UUID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = detResp.Body.Close() }()
	var det PaymentDetail
	if err := json.NewDecoder(detResp.Body).Decode(&det); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(det.Customer, envoye) {
		t.Errorf("customer restitue different apres aller-retour SQLite\n obtenu : %+v\n veut   : %+v",
			det.Customer, envoye)
	}
}

// Le filtre par token répond à « qu'a-t-on fait avec cet alias » : c'est
// la lecture inverse de PaymentSummary.PaymentMethodToken, celle dont la
// fiche d'un moyen a besoin.
func TestListPaymentsFiltreParToken(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	enrol, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "ENROL",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "5555555555554444", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	r1, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(enrol))
	var enrolled CreatePaymentOutput
	_ = json.NewDecoder(r1.Body).Decode(&enrolled)
	_ = r1.Body.Close()
	token := enrolled.PaymentMethodToken

	// Un rejeu sur ce moyen, et un paiement sans rapport.
	replay, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1990, Currency: "EUR", OrderID: "REPLAY",
		PaymentMethodToken: token,
	})
	r2, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(replay))
	_ = r2.Body.Close()

	autre, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 500, Currency: "EUR", OrderID: "SANS-LIEN",
	})
	r3, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(autre))
	_ = r3.Body.Close()

	resp, err := http.Get(server.URL + "/paysim/api/v1/payments?paymentMethodToken=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var filtres []PaymentSummary
	if err := json.NewDecoder(resp.Body).Decode(&filtres); err != nil {
		t.Fatal(err)
	}

	if len(filtres) != 2 {
		t.Fatalf("%d paiements, veut 2 (enrolement + rejeu)", len(filtres))
	}
	for _, p := range filtres {
		if p.PaymentMethodToken != token {
			t.Errorf("%s : token = %q, veut %q", p.OrderID, p.PaymentMethodToken, token)
		}
		if p.OrderID == "SANS-LIEN" {
			t.Error("un paiement sans rapport a franchi le filtre")
		}
	}
}

// Sans filtre, tout ressort : le paramètre est optionnel, il ne doit pas
// changer le comportement par défaut.
func TestListPaymentsSansFiltreRetourneTout(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	for _, ord := range []string{"A", "B"} {
		body, _ := json.Marshal(CreatePaymentInput{
			Provider: "payzen", Amount: 100, Currency: "EUR", OrderID: ord,
		})
		r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
			"application/json", bytes.NewReader(body))
		_ = r.Body.Close()
	}

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments")
	defer func() { _ = resp.Body.Close() }()
	var tous []PaymentSummary
	_ = json.NewDecoder(resp.Body).Decode(&tous)
	if len(tous) != 2 {
		t.Errorf("%d paiements, veut 2", len(tous))
	}
}

// Le token doit accompagner le paiement dès la liste : c'est lui qui
// alimente la colonne et le lien vers la fiche du moyen.
func TestPaymentSummaryPorteLeToken(t *testing.T) {
	t.Parallel()
	server, _ := setupWithPayzen(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "ENROL-TOK",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var out CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&out)
	_ = r.Body.Close()

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments")
	defer func() { _ = resp.Body.Close() }()
	var liste []PaymentSummary
	_ = json.NewDecoder(resp.Body).Decode(&liste)

	if len(liste) != 1 || liste[0].PaymentMethodToken != out.PaymentMethodToken {
		t.Errorf("token absent du resume : %+v", liste)
	}
}

// Le filtre par abonnement repond a « qu'a prelevé cette souscription ».
// Le rattachement vit dans les metadonnees du paiement, que le resume
// n'expose pas : sans filtre serveur, la fiche d'un abonnement ne peut
// pas retrouver ses echeances.
func TestListPaymentsFiltreParSubscription(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	creer := func(orderID, subID string) {
		in := CreatePaymentInput{
			Provider: "payzen", Amount: 1990, Currency: "EUR", OrderID: orderID,
		}
		if subID != "" {
			in.Metadata = map[string]string{"subscriptionId": subID}
		}
		body, _ := json.Marshal(in)
		r, err := http.Post(server.URL+"/paysim/api/v1/payments",
			"application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
	}
	creer("ECH-1", "sub-A")
	creer("ECH-2", "sub-A")
	creer("AUTRE-SUB", "sub-B")
	creer("HORS-ABO", "")

	resp, err := http.Get(server.URL + "/paysim/api/v1/payments?subscriptionId=sub-A")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var filtres []PaymentSummary
	if err := json.NewDecoder(resp.Body).Decode(&filtres); err != nil {
		t.Fatal(err)
	}

	if len(filtres) != 2 {
		t.Fatalf("%d paiements, veut 2 (les deux echeances de sub-A)", len(filtres))
	}
	for _, p := range filtres {
		if p.OrderID != "ECH-1" && p.OrderID != "ECH-2" {
			t.Errorf("%s a franchi le filtre", p.OrderID)
		}
	}
}

// Un identifiant inconnu ne doit pas se comporter comme un filtre absent :
// repondre la liste entiere ferait passer tous les paiements pour les
// echeances de cet abonnement.
func TestListPaymentsSubscriptionInconnue(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 100, Currency: "EUR", OrderID: "SEUL",
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	_ = r.Body.Close()

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payments?subscriptionId=sub-fantome")
	defer func() { _ = resp.Body.Close() }()
	var filtres []PaymentSummary
	_ = json.NewDecoder(resp.Body).Decode(&filtres)
	if len(filtres) != 0 {
		t.Errorf("%d paiements, veut 0", len(filtres))
	}
}

// Le compteur d'echeances distingue en liste un abonnement qui preleve
// d'un abonnement qui n'a encore rien produit — la question qu'on se pose
// justement quand une facturation recurrente ne tombe pas.
func TestSubscriptionBillingCount(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	enrol, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "INIT",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(enrol))
	var created CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&created)
	_ = r.Body.Close()

	subBody, _ := json.Marshal(CreateSubscriptionInput{
		PaymentMethodToken: created.PaymentMethodToken,
		Amount:             2990, Currency: "EUR", OrderID: "SUB",
		EffectDate: "2026-09-01", Rrule: "RRULE:FREQ=MONTHLY",
	})
	subResp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(subBody))
	var sub SubscriptionOutput
	_ = json.NewDecoder(subResp.Body).Decode(&sub)
	_ = subResp.Body.Close()

	if sub.BillingCount != 0 {
		t.Errorf("a la creation, billingCount = %d, veut 0", sub.BillingCount)
	}

	for _, ord := range []string{"ECH-1", "ECH-2", "ECH-3"} {
		body, _ := json.Marshal(CreatePaymentInput{
			Provider: "payzen", Amount: 2990, Currency: "EUR", OrderID: ord,
			Metadata: map[string]string{"subscriptionId": sub.ID},
		})
		p, _ := http.Post(server.URL+"/paysim/api/v1/payments",
			"application/json", bytes.NewReader(body))
		_ = p.Body.Close()
	}

	resp, _ := http.Get(server.URL + "/paysim/api/v1/subscriptions")
	defer func() { _ = resp.Body.Close() }()
	var liste []SubscriptionOutput
	if err := json.NewDecoder(resp.Body).Decode(&liste); err != nil {
		t.Fatal(err)
	}
	if len(liste) != 1 {
		t.Fatalf("%d abonnements, veut 1", len(liste))
	}
	if liste[0].BillingCount != 3 {
		t.Errorf("billingCount = %d, veut 3", liste[0].BillingCount)
	}
}

// Reproduction du cas remonte : l'expiration fournie a la creation doit
// se retrouver sur l'alias. Sans elle, l'alias nait en 0/0 et tout ce qui
// en derive annonce une expiration qui n'a pas eu lieu.
func TestEnrolementConserveExpiration(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "ENROL-EXP",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var out CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&out)
	_ = r.Body.Close()
	t.Logf("creation : state=%s token=%q declineCode=%q", out.State, out.PaymentMethodToken, out.DeclineCode)

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	defer func() { _ = resp.Body.Close() }()
	var liste []PaymentMethodOutput
	if err := json.NewDecoder(resp.Body).Decode(&liste); err != nil {
		t.Fatal(err)
	}
	if len(liste) != 1 {
		t.Fatalf("%d moyens, veut 1", len(liste))
	}
	m := liste[0]
	t.Logf("alias : pan=%s exp=%d/%d usable=%v raison=%q",
		m.PANMasked, m.ExpiryMonth, m.ExpiryYear, m.Usable, m.UnusableReason)
	if m.ExpiryMonth != 12 || m.ExpiryYear != 2028 {
		t.Errorf("expiration = %d/%d, veut 12/2028", m.ExpiryMonth, m.ExpiryYear)
	}
	// La cause retenue doit etre le PAN de test, pas une expiration.
	if m.UnusableReason != "carte de test refusee" {
		t.Errorf("raison = %q, veut « carte de test refusee »", m.UnusableReason)
	}
}

// « L'alias (token) ne sera pas cree si la demande d'autorisation ou de
// renseignement est refusee » — guide Lyra, Paiements par token.
//
// Un refus ne laisse donc aucun alias derriere lui. Le masquer a
// l'affichage ne suffisait pas : la relecture du paiement le montrait
// quand meme, et un integrateur pouvait conserver un alias que le vrai
// PSP n'avait jamais attribue.
func TestPaiementRefuseNeLaisseAucunAlias(t *testing.T) {
	t.Parallel()
	server, store := setupWithRepos(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 5000, Currency: "EUR", OrderID: "REFUS-ALIAS",
		FormAction: "REGISTER_PAY",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	var out CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&out)
	_ = r.Body.Close()

	simulerPaiement(t, server, out.UUID, payzen.OutcomeUnpaid)

	// Ni sur la transaction, ni dans la collection, ni dans le detail.
	tx, _ := store.ByUUID(out.UUID)
	if tx == nil {
		t.Fatal("transaction introuvable")
	}
	if tx.PaymentMethodToken != "" {
		t.Errorf("la transaction porte un alias apres un refus : %q", tx.PaymentMethodToken)
	}

	resp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	defer func() { _ = resp.Body.Close() }()
	var moyens []PaymentMethodOutput
	_ = json.NewDecoder(resp.Body).Decode(&moyens)
	if len(moyens) != 0 {
		t.Errorf("%d alias apres un refus, veut 0 : %+v", len(moyens), moyens)
	}

	det, _ := http.Get(server.URL + "/paysim/api/v1/payments/" + out.UUID)
	defer func() { _ = det.Body.Close() }()
	var detail PaymentDetail
	_ = json.NewDecoder(det.Body).Decode(&detail)
	if detail.PaymentMethodToken != "" {
		t.Errorf("le detail annonce un alias apres un refus : %q", detail.PaymentMethodToken)
	}
}

// L'enrolement sans paiement se tranche tout de suite : il n'y a pas de
// porteur a attendre. C'est la transaction de VERIFICATION de Lyra.
func TestEnrolementSansPaiementRendUnAliasImmediatement(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "VERIF",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = r.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&out)

	if out.PaymentMethodToken == "" {
		t.Fatal("aucun alias : une verification sans debit n'a personne a attendre")
	}
	// Chez Lyra, la transaction de VERIFICATION porte un statut —
	// « Accepte » ou « Refuse » — et « n'est jamais remise en banque ».
	// Autorisee, donc, jamais capturee. La laisser « initiated » ferait
	// croire qu'on attend encore le porteur.
	if out.State != "authorized" {
		t.Errorf("state = %q, veut authorized (verification acceptee)", out.State)
	}
}

// Une verification refusee est visible comme telle : c'est le role que
// PayZen donne a cette transaction — « aider le marchand a comprendre
// les raisons du refus de la creation de l'alias ».
func TestEnrolementSansPaiementRefuseEstVisible(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	token := enroll(t, server, "4111111111111111", 12, 2030)
	expireMethod(t, server, token)

	// Une carte perimee presentee a un nouvel enrolement : refus.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "VERIF-KO",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 1, ExpiryYear: 2020},
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = r.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&out)

	if out.State != "declined" {
		t.Errorf("state = %q, veut declined (verification refusee)", out.State)
	}
	if out.PaymentMethodToken != "" {
		t.Errorf("alias rendu malgre le refus : %q", out.PaymentMethodToken)
	}
}

// Une verification ne debite pas : elle ne peut donc pas echouer pour
// provision insuffisante. C'est ce qui rend l'alias « qui refusera aux
// echeances » obtenable — le levier de test du recurrent.
func TestEnrolementSansPaiementAccepteUnPANDeRefus(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	token := enroll(t, server, "4000000000000002", 12, 2030)
	if token == "" {
		t.Fatal("PAN de refus non enrole : le levier du recurrent disparait")
	}

	// Et il refuse bien au debit.
	body, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 2990, Currency: "EUR", OrderID: "DEBIT",
		PaymentMethodToken: token,
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(body))
	defer func() { _ = r.Body.Close() }()
	var out CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&out)
	if out.State != "declined" || out.DeclineCode != "51" {
		t.Errorf("state/code = %q/%q, veut declined/51", out.State, out.DeclineCode)
	}
}

// Une carte sans expiration doit echouer bruyamment. Acceptee, elle
// produisait un alias en 0/0 aussitot repute expire : le marchand
// cherchait alors une date perimee qu'il n'avait jamais envoyee.
func TestEnrolementCarteSansExpirationRefuse(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	cas := []struct {
		nom  string
		card string
	}{
		{"expiration absente", `{"pan":"4111111111111111"}`},
		{"mois hors plage", `{"pan":"4111111111111111","expiryMonth":13,"expiryYear":2030}`},
		{"annee sur deux chiffres", `{"pan":"4111111111111111","expiryMonth":12,"expiryYear":30}`},
		{"pan absent", `{"expiryMonth":12,"expiryYear":2030}`},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			body := []byte(`{"provider":"payzen","amount":1000,"currency":"EUR",` +
				`"orderId":"REFUS","formAction":"REGISTER_PAY","card":` + c.card + `}`)
			r, err := http.Post(server.URL+"/paysim/api/v1/payments",
				"application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Body.Close() }()
			if r.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, veut 400", r.StatusCode)
			}
		})
	}

	// Ni paiement ni alias : un enrolement refuse ne laisse rien derriere.
	resp, _ := http.Get(server.URL + "/paysim/api/v1/payment-methods")
	defer func() { _ = resp.Body.Close() }()
	var moyens []PaymentMethodOutput
	_ = json.NewDecoder(resp.Body).Decode(&moyens)
	if len(moyens) != 0 {
		t.Errorf("%d moyens crees, veut 0", len(moyens))
	}

	pResp, _ := http.Get(server.URL + "/paysim/api/v1/payments")
	defer func() { _ = pResp.Body.Close() }()
	var paiements []PaymentSummary
	_ = json.NewDecoder(pResp.Body).Decode(&paiements)
	if len(paiements) != 0 {
		t.Errorf("%d paiements crees, veut 0", len(paiements))
	}
}

// Le motif accompagne le refus des la creation. Sans lui, un integrateur
// qui recoit « declined » doit relire le paiement pour savoir s'il peut
// reconduire — un aller-retour que le protocole imite ne demande pas.
func TestCreatePaymentPorteLeMotifDuRefus(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	enrol, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "ENROL",
		FormAction: "REGISTER",
		Card:       &payzen.Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	r, _ := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(enrol))
	var enrolled CreatePaymentOutput
	_ = json.NewDecoder(r.Body).Decode(&enrolled)
	_ = r.Body.Close()

	// Rejeu one-click sur un montant magique : refus immediat en 51.
	replay, _ := json.Marshal(CreatePaymentInput{
		Provider: "payzen", Amount: 1001, Currency: "EUR", OrderID: "REPLAY-51",
		PaymentMethodToken: enrolled.PaymentMethodToken,
	})
	r2, err := http.Post(server.URL+"/paysim/api/v1/payments",
		"application/json", bytes.NewReader(replay))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r2.Body.Close() }()
	var out CreatePaymentOutput
	if err := json.NewDecoder(r2.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	if out.State != "declined" {
		t.Fatalf("state = %q, veut declined", out.State)
	}
	if out.DeclineCode != "51" {
		t.Errorf("declineCode = %q, veut 51", out.DeclineCode)
	}
	if out.DeclineMessage == "" {
		t.Error("declineMessage vide")
	}
}

