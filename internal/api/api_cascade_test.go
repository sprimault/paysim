// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/delivery"
)

// Supprimer un paiement sans ses livraisons ne servait à rien : elles
// restaient à l'écran, rattachées à un paiement introuvable. Ces tests
// verrouillent la cascade, et surtout ce qu'elle ne doit PAS emporter —
// les livraisons sans paiement, qu'un webhook sans transaction produit
// légitimement.

// livrer met en file des webhooks et attend qu'ils soient historisés.
// Passer par la file plutôt que par un simulate : le test porte sur la
// suppression, pas sur la fabrication d'un kr-answer.
func livrer(t *testing.T, server *httptest.Server, queue *delivery.Queue,
	entrees []struct{ id, payment string }) {
	t.Helper()
	aval := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(aval.Close)

	for _, e := range entrees {
		if err := queue.Enqueue(delivery.Webhook{
			ID: e.id, URL: aval.URL, Body: []byte(`{"x":1}`),
			Headers:     map[string]string{"Content-Type": "application/json"},
			PaymentUUID: e.payment,
		}); err != nil {
			t.Fatalf("Enqueue %s: %v", e.id, err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(listerWebhooks(t, server, "")) == len(entrees) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("les %d livraisons ne sont pas historisees", len(entrees))
}

func listerWebhooks(t *testing.T, server *httptest.Server, query string) []WebhookEntry {
	t.Helper()
	resp, err := http.Get(server.URL + "/paysim/api/v1/webhooks" + query)
	if err != nil {
		t.Fatalf("GET webhooks%s: %v", query, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out []WebhookEntry
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func supprimer(t *testing.T, server *httptest.Server, chemin string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/paysim/api/v1"+chemin, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", chemin, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestDeletePaiement_cascadeSesLivraisons(t *testing.T) {
	t.Parallel()
	server, _, queue, _ := setup(t, "")
	livrer(t, server, queue, []struct{ id, payment string }{
		{"wh-a1", "pay-a"}, {"wh-a2", "pay-a"},
		{"wh-b1", "pay-b"},
		{"wh-orph", ""},
	})

	if code := supprimer(t, server, "/payments/pay-a"); code != http.StatusNoContent {
		t.Errorf("statut = %d, veut 204", code)
	}

	restants := listerWebhooks(t, server, "")
	if len(restants) != 2 {
		t.Fatalf("restants = %d, veut 2 (pay-b et l'orpheline)", len(restants))
	}
	for _, e := range restants {
		if e.PaymentUUID == "pay-a" {
			t.Errorf("une livraison de pay-a a survecu : %s", e.ID)
		}
	}
	if n := len(listerWebhooks(t, server, "?paymentUuid=pay-b")); n != 1 {
		t.Errorf("pay-b a perdu ses livraisons : %d restante(s)", n)
	}
}

// Un UUID inconnu reste un 204 : la route est idempotente, et une
// cascade sans cible ne doit pas la faire basculer en erreur.
func TestDeletePaiement_inconnuResteIdempotent(t *testing.T) {
	t.Parallel()
	server, _, queue, _ := setup(t, "")
	livrer(t, server, queue, []struct{ id, payment string }{{"wh-a1", "pay-a"}})

	if code := supprimer(t, server, "/payments/inexistant"); code != http.StatusNoContent {
		t.Errorf("statut = %d, veut 204", code)
	}
	if n := len(listerWebhooks(t, server, "")); n != 1 {
		t.Errorf("une suppression sans cible a emporte %d livraison(s)", 1-n)
	}
}

// La purge emporte les livraisons des paiements, jamais les orphelines.
// C'est ce test qui interdit de revenir au raccourci « vider tout
// l'historique », plus simple et faux.
func TestPurgePaiements_cascadeEtEpargneLesOrphelines(t *testing.T) {
	t.Parallel()
	server, _, queue, _ := setup(t, "")
	livrer(t, server, queue, []struct{ id, payment string }{
		{"wh-a1", "pay-a"}, {"wh-b1", "pay-b"}, {"wh-orph", ""},
	})

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/paysim/api/v1/payments", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE payments: %v", err)
	}
	var out PurgePaymentsOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()

	if out.Webhooks != 2 {
		t.Errorf("webhooks = %d, veut 2", out.Webhooks)
	}
	if out.Partial {
		t.Error("purge annoncee partielle sans echec")
	}
	restants := listerWebhooks(t, server, "")
	if len(restants) != 1 || restants[0].ID != "wh-orph" {
		t.Errorf("l'orpheline devait survivre, restants = %+v", restants)
	}
}
