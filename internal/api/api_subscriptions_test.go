// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprimault/paysim/internal/format"
)

// createSubHelper crée un abonnement via l'API et retourne son ID.
// Factorisé pour éviter la duplication dans les tests trigger/cancel.
func createSubHelper(t *testing.T, server *httptest.Server, token string, amount int64) string {
	t.Helper()
	body, _ := json.Marshal(CreateSubscriptionInput{
		PaymentMethodToken: token,
		Amount:             format.Amount(amount),
		Currency:           "EUR",
		OrderID:            "SUB",
		EffectDate:         "2026-09-01T00:00:00Z",
		Rrule:              "RRULE:FREQ=MONTHLY;INTERVAL=1",
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("createSub POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("createSub status = %d, veut 201", resp.StatusCode)
	}
	var out SubscriptionOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.ID
}

func TestCreateSubscription_nominal(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")
	token := enroll(t, server, "4111111111111111", 12, 2028)

	body, _ := json.Marshal(CreateSubscriptionInput{
		PaymentMethodToken: token,
		Amount:             2990,
		Currency:           "EUR",
		OrderID:            "SUB-42",
		EffectDate:         "2026-09-01T00:00:00Z",
		Rrule:              "RRULE:FREQ=MONTHLY;INTERVAL=1",
		Metadata:           map[string]string{"plan": "pro"},
	})
	resp, err := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, veut 201", resp.StatusCode)
	}
	var out SubscriptionOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.ID == "" {
		t.Errorf("ID vide")
	}
	if out.Provider != "payzen" || out.PaymentMethodToken != token {
		t.Errorf("Provider/Token = %q/%q", out.Provider, out.PaymentMethodToken)
	}
	if out.Amount != 2990 || out.Rrule != "RRULE:FREQ=MONTHLY;INTERVAL=1" {
		t.Errorf("Amount/Rrule = %d/%q", out.Amount, out.Rrule)
	}
	if out.Cancelled {
		t.Errorf("Cancelled = true a la creation")
	}
}

func TestCreateSubscription_tokenInconnu(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	body, _ := json.Marshal(CreateSubscriptionInput{
		PaymentMethodToken: "does-not-exist",
		Amount:             1000, Currency: "EUR", OrderID: "X",
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400 (payment method inconnu)", resp.StatusCode)
	}
}

func TestCreateSubscription_amountInvalide(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	token := enroll(t, server, "4111111111111111", 12, 2028)
	body, _ := json.Marshal(CreateSubscriptionInput{
		PaymentMethodToken: token, Amount: 0, Currency: "EUR", OrderID: "X",
	})
	resp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400 (amount invalide)", resp.StatusCode)
	}
}

func TestCreateSubscription_providerDefautPayzen(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")
	token := enroll(t, server, "4111111111111111", 12, 2028)

	body := []byte(`{
		"paymentMethodToken":"` + token + `",
		"amount":1500, "currency":"EUR", "orderId":"D",
		"effectDate":"2026-09-01", "rrule":"RRULE:FREQ=MONTHLY"
	}`)
	resp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions",
		"application/json", bytes.NewReader(body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, veut 201", resp.StatusCode)
	}
	var out SubscriptionOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Provider != "payzen" {
		t.Errorf("Provider = %q, veut payzen (defaut)", out.Provider)
	}
}

func TestGetSubscription_nominal(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")
	token := enroll(t, server, "4111111111111111", 12, 2028)
	id := createSubHelper(t, server, token, 1500)

	resp, err := http.Get(server.URL + "/paysim/api/v1/subscriptions/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", resp.StatusCode)
	}
	var out SubscriptionOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.ID != id || out.Amount != 1500 {
		t.Errorf("out = %+v", out)
	}
}

func TestGetSubscription_inconnu(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	resp, _ := http.Get(server.URL + "/paysim/api/v1/subscriptions/does-not-exist")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, veut 404", resp.StatusCode)
	}
}

func TestTriggerBilling_captureNominal(t *testing.T) {
	t.Parallel()
	server, store := setupWithRepos(t, "")
	token := enroll(t, server, "4111111111111111", 12, 2028)
	id := createSubHelper(t, server, token, 2000)

	resp, err := http.Post(server.URL+"/paysim/api/v1/subscriptions/"+id+"/trigger-billing",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, veut 201", resp.StatusCode)
	}
	var out TriggerBillingOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.State != "captured" {
		t.Errorf("state = %q, veut captured", out.State)
	}
	if out.SubscriptionID != id || out.PaymentUUID == "" {
		t.Errorf("out = %+v", out)
	}
	// La Transaction doit être en base avec metadata subscriptionId.
	tx, _ := store.ByUUID(out.PaymentUUID)
	if tx == nil || tx.Metadata["subscriptionId"] != id {
		t.Errorf("metadata subscriptionId = %v, veut %q", tx.Metadata, id)
	}
}

func TestTriggerBilling_refusMagicPAN(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")
	token := enroll(t, server, "4000000000000002", 12, 2028)
	id := createSubHelper(t, server, token, 1000)

	resp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions/"+id+"/trigger-billing",
		"application/json", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, veut 201", resp.StatusCode)
	}
	var out TriggerBillingOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.State != "declined" {
		t.Errorf("state = %q, veut declined (magic PAN)", out.State)
	}
}

func TestTriggerBilling_refusExpiration(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")
	token := enroll(t, server, "4111111111111111", 1, 2020)
	id := createSubHelper(t, server, token, 1000)

	resp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions/"+id+"/trigger-billing",
		"application/json", nil)
	defer func() { _ = resp.Body.Close() }()
	var out TriggerBillingOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.State != "declined" {
		t.Errorf("state = %q, veut declined (expiration)", out.State)
	}
}

func TestTriggerBilling_apresCancel(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")
	token := enroll(t, server, "4111111111111111", 12, 2028)
	id := createSubHelper(t, server, token, 1000)

	cancelResp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions/"+id+"/cancel",
		"application/json", nil)
	_ = cancelResp.Body.Close()
	if cancelResp.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel status = %d, veut 204", cancelResp.StatusCode)
	}
	triggerResp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions/"+id+"/trigger-billing",
		"application/json", nil)
	defer func() { _ = triggerResp.Body.Close() }()
	if triggerResp.StatusCode != http.StatusBadRequest {
		t.Errorf("trigger apres cancel = %d, veut 400", triggerResp.StatusCode)
	}
}

func TestTriggerBilling_subscriptionInconnue(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	resp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions/does-not-exist/trigger-billing",
		"application/json", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, veut 404", resp.StatusCode)
	}
}

func TestCancelSubscription_idempotent(t *testing.T) {
	t.Parallel()
	server, _ := setupWithRepos(t, "")

	// Sur ID inconnu : 204 (idempotent).
	resp, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions/unknown/cancel",
		"application/json", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("cancel inconnu = %d, veut 204", resp.StatusCode)
	}
	// Sur ID connu, deux appels → toujours 204.
	token := enroll(t, server, "4111111111111111", 12, 2028)
	id := createSubHelper(t, server, token, 1000)
	for i := 0; i < 2; i++ {
		r, _ := http.Post(server.URL+"/paysim/api/v1/subscriptions/"+id+"/cancel",
			"application/json", nil)
		_ = r.Body.Close()
		if r.StatusCode != http.StatusNoContent {
			t.Errorf("cancel #%d = %d, veut 204", i+1, r.StatusCode)
		}
	}
}
