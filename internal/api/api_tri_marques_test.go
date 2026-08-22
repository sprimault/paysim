// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/clock"
	"github.com/sprimault/paysim/internal/providers/payzen"
	"github.com/sprimault/paysim/internal/store"
	"github.com/sprimault/paysim/internal/store/inmem"
)

// Ordre des listes quand plusieurs marques Lyra cohabitent.
//
// Les dépôts lisent par une marque à la fois, et l'adaptateur en compte
// cinq : une liste globale concatène donc cinq lectures. Chacune arrive
// triée, mais leur concaténation ne l'est pas — elle est groupée par
// marque. Le défaut est invisible sur une instance mono-marque, où
// quatre lectures sur cinq rendent vide, et c'est ce qui l'a laissé
// passer : la liste des paiements retriait, ces deux-là non.
//
// Les enregistrements sont posés dans un ordre entrelacé exprès : le
// plus récent appartient à la deuxième marque, le plus ancien à la
// première. Un résultat groupé par marque échoue, un résultat trié
// passe.

// testHandlerAvecDepots monte l'API comme le fait la branche mémoire de
// main.go : les trois dépôts et le wrapper payzen. Un handler payzen est
// indispensable ici — sans lui, listSubscriptions court-circuite et rend
// une liste vide, ce qui ferait passer le test pour une mauvaise raison.
func testHandlerAvecDepots(t *testing.T, subs *inmem.SubscriptionsRepository, methods *inmem.PaymentMethodsRepository) *httptest.Server {
	t.Helper()
	logger := discardLogger()
	payments := inmem.NewPaymentsRepository(0, nil)
	payzenStore := payzen.NewRepoStore(clock.System{}, payments, subs, methods)
	ph := payzen.NewHandler(payzenStore, nil, logger, clock.System{}, payzen.HandlerConfig{
		HMACKey: "k", RESTPassword: "pwd-rest",
	})
	h := NewHandler(Deps{
		Store:             payzenStore,
		PaymentRepo:       payments,
		SubscriptionRepo:  subs,
		PaymentMethodRepo: methods,
		PayzenHandler:     ph,
		Publisher:         bus.New(),
		Logger:            logger,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestListeAbonnementsTrieeToutesMarquesConfondues(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	subs := inmem.NewSubscriptionsRepository()
	// Ordre d'insertion volontairement décorrélé de l'ordre attendu.
	for _, cas := range []struct {
		id       string
		provider string
		maj      time.Time
	}{
		{"sub-payzen-vieux", "payzen", base.Add(-3 * time.Hour)},
		{"sub-systempay-recent", "systempay", base},
		{"sub-payzen-moyen", "payzen", base.Add(-2 * time.Hour)},
		{"sub-scellius-second", "scellius", base.Add(-1 * time.Hour)},
	} {
		if err := subs.Save(&store.SubscriptionRecord{
			ID: cas.id, Provider: cas.provider, OrderID: cas.id,
			Amount: 1000, Currency: "EUR",
			CreatedAt: cas.maj, UpdatedAt: cas.maj,
		}); err != nil {
			t.Fatal(err)
		}
	}

	srv := testHandlerAvecDepots(t, subs, inmem.NewPaymentMethodsRepository())
	resp, err := http.Get(srv.URL + "/paysim/api/v1/subscriptions")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	veut := []string{"sub-systempay-recent", "sub-scellius-second", "sub-payzen-moyen", "sub-payzen-vieux"}
	if len(out) != len(veut) {
		t.Fatalf("%d abonnements rendus, attendu %d", len(out), len(veut))
	}
	for i, id := range veut {
		if out[i].ID != id {
			t.Errorf("position %d = %q, attendu %q — la liste est groupée par marque au lieu d'être triée",
				i, out[i].ID, id)
		}
	}
}

func TestListeMoyensTrieeToutesMarquesConfondues(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	methods := inmem.NewPaymentMethodsRepository()
	for _, cas := range []struct {
		token    string
		provider string
		cree     time.Time
	}{
		{"tok-payzen-vieux", "payzen", base.Add(-3 * time.Hour)},
		{"tok-sogecommerce-recent", "sogecommerce", base},
		{"tok-payzen-moyen", "payzen", base.Add(-2 * time.Hour)},
		{"tok-systempay-second", "systempay", base.Add(-1 * time.Hour)},
	} {
		if err := methods.Save(&store.PaymentMethodRecord{
			Token: cas.token, Provider: cas.provider,
			PANMasked: "497010******0154", ExpiryMonth: 12, ExpiryYear: 2030,
			Brand: "VISA", CreatedAt: cas.cree,
		}); err != nil {
			t.Fatal(err)
		}
	}

	srv := testHandlerAvecDepots(t, inmem.NewSubscriptionsRepository(), methods)
	resp, err := http.Get(srv.URL + "/paysim/api/v1/payment-methods")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out []struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	veut := []string{"tok-sogecommerce-recent", "tok-systempay-second", "tok-payzen-moyen", "tok-payzen-vieux"}
	if len(out) != len(veut) {
		t.Fatalf("%d moyens rendus, attendu %d", len(out), len(veut))
	}
	for i, token := range veut {
		if out[i].Token != token {
			t.Errorf("position %d = %q, attendu %q — la liste est groupée par marque au lieu d'être triée",
				i, out[i].Token, token)
		}
	}
}
