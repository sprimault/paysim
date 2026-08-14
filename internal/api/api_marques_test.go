// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Les trois collections exposent le même filtre ?provider=. Il a été
// ajouté au coup par coup, et à chaque fois la lecture l'a ignoré
// pendant un temps : le paramètre passait, la réponse rendait tout, et
// rien ne le signalait. Les trois tests vivent ici ensemble pour que
// l'oubli de la quatrième collection se voie.

// marquesTest est le jeu commun : trois marques suffisent à distinguer
// « filtré » de « tout rendu », et l'une d'elles n'est pas celle par
// défaut, ce qui est précisément le cas qui cassait.
var marquesTest = []string{"payzen", "systempay", "scellius"}

// postJSON envoie corps sur chemin et décode la réponse dans dest.
func postJSON(t *testing.T, server *httptest.Server, chemin, corps string, dest any) {
	t.Helper()
	resp, err := http.Post(server.URL+"/paysim/api/v1"+chemin,
		"application/json", bytes.NewBufferString(corps))
	if err != nil {
		t.Fatalf("POST %s : %v", chemin, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s : statut %d", chemin, resp.StatusCode)
	}
	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			t.Fatalf("POST %s : decodage : %v", chemin, err)
		}
	}
}

// getJSON lit chemin et décode la réponse dans dest.
func getJSON(t *testing.T, server *httptest.Server, chemin string, dest any) {
	t.Helper()
	resp, err := http.Get(server.URL + "/paysim/api/v1" + chemin)
	if err != nil {
		t.Fatalf("GET %s : %v", chemin, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		t.Fatalf("GET %s : decodage : %v", chemin, err)
	}
}

// TestListPayments_filtreParMarque : le paramètre était accepté et
// ignoré, donc la liste rendait tout. Un appelant qui filtre sur une
// marque recevait les autres sans rien pour le lui dire.
func TestListPayments_filtreParMarque(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	for _, marque := range marquesTest {
		postJSON(t, server, "/payments",
			`{"provider":"`+marque+`","amount":1000,"currency":"EUR","orderId":"CMD-`+marque+`"}`, nil)
	}

	lire := func(query string) []PaymentSummary {
		t.Helper()
		var out []PaymentSummary
		getJSON(t, server, "/payments"+query, &out)
		return out
	}

	if got := lire(""); len(got) != len(marquesTest) {
		t.Fatalf("sans filtre = %d paiements, veut %d", len(got), len(marquesTest))
	}
	for _, marque := range marquesTest {
		got := lire("?provider=" + marque)
		if len(got) != 1 {
			t.Errorf("filtre %s = %d paiements, veut 1", marque, len(got))
			continue
		}
		if got[0].Provider != marque {
			t.Errorf("filtre %s rend un paiement %s", marque, got[0].Provider)
		}
	}
	if got := lire("?provider=monetico"); len(got) != 0 {
		t.Errorf("filtre sur une marque absente = %d paiements, veut 0", len(got))
	}
}

// Même contrat sur les alias. Deux défauts se cumulaient ici : la
// marque n'était pas persistée — tout alias était rangé chez PayZen —
// et le filtre n'était pas lu. Corriger l'un sans l'autre ne se voit
// pas, d'où le test qui exige les deux.
func TestListPaymentMethods_filtreParMarque(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	for _, marque := range marquesTest {
		postJSON(t, server, "/payments",
			`{"provider":"`+marque+`","amount":0,"currency":"EUR","orderId":"ENROL-`+marque+`",
			  "formAction":"REGISTER",
			  "card":{"pan":"4111111111111111","expiryMonth":12,"expiryYear":2030}}`, nil)
	}

	lire := func(query string) []PaymentMethodOutput {
		t.Helper()
		var out []PaymentMethodOutput
		getJSON(t, server, "/payment-methods"+query, &out)
		return out
	}

	if got := lire(""); len(got) != len(marquesTest) {
		t.Fatalf("sans filtre = %d alias, veut %d", len(got), len(marquesTest))
	}
	for _, marque := range marquesTest {
		got := lire("?provider=" + marque)
		if len(got) != 1 {
			t.Errorf("filtre %s = %d alias, veut 1", marque, len(got))
			continue
		}
		if got[0].Provider != marque {
			t.Errorf("filtre %s rend un alias %s", marque, got[0].Provider)
		}
	}
	if got := lire("?provider=monetico"); len(got) != 0 {
		t.Errorf("filtre sur une marque absente = %d alias, veut 0", len(got))
	}
}

// Sur les abonnements, la godoc annonçait le filtre depuis l'origine
// sans qu'aucune ligne ne l'applique — le cas le plus coûteux, puisque
// la documentation faisait foi.
func TestListSubscriptions_filtreParMarque(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	for _, marque := range marquesTest {
		var enrol CreatePaymentOutput
		postJSON(t, server, "/payments",
			`{"provider":"`+marque+`","amount":0,"currency":"EUR","orderId":"ENROL-`+marque+`",
			  "formAction":"REGISTER",
			  "card":{"pan":"4111111111111111","expiryMonth":12,"expiryYear":2030}}`, &enrol)
		postJSON(t, server, "/subscriptions",
			`{"provider":"`+marque+`","paymentMethodToken":"`+enrol.PaymentMethodToken+`",
			  "amount":1990,"currency":"EUR","orderId":"SUB-`+marque+`"}`, nil)
	}

	lire := func(query string) []SubscriptionOutput {
		t.Helper()
		var out []SubscriptionOutput
		getJSON(t, server, "/subscriptions"+query, &out)
		return out
	}

	if got := lire(""); len(got) != len(marquesTest) {
		t.Fatalf("sans filtre = %d abonnements, veut %d", len(got), len(marquesTest))
	}
	for _, marque := range marquesTest {
		got := lire("?provider=" + marque)
		if len(got) != 1 {
			t.Errorf("filtre %s = %d abonnements, veut 1", marque, len(got))
			continue
		}
		if got[0].Provider != marque {
			t.Errorf("filtre %s rend un abonnement %s", marque, got[0].Provider)
		}
	}
	if got := lire("?provider=monetico"); len(got) != 0 {
		t.Errorf("filtre sur une marque absente = %d abonnements, veut 0", len(got))
	}
}
