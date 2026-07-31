// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	store := NewStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(NewHandler(store, logger))
	t.Cleanup(server.Close)
	return server, store
}

// post envoie une requete authentifiee (Basic Auth par defaut) et
// decode la reponse en APIResponse. Concentre le boilerplate des
// tests HTTP.
func post(t *testing.T, url string, body any, user, pass string) (*APIResponse, int) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body : %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request : %v", err)
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out APIResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode reponse : %v", err)
		}
	}
	return &out, resp.StatusCode
}

func TestCreatePaymentSuccess(t *testing.T) {
	t.Parallel()
	server, store := newTestServer(t)

	body := CreatePaymentRequest{
		OrderID:  "order-42",
		Amount:   1500,
		Currency: "EUR",
	}
	resp, status := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment", body, "u", "p")

	if status != http.StatusOK {
		t.Fatalf("status HTTP = %d, veut 200", status)
	}
	if resp.Status != "SUCCESS" {
		t.Errorf("Status = %q, veut SUCCESS", resp.Status)
	}
	var answer CreatePaymentAnswer
	if err := json.Unmarshal(resp.Answer, &answer); err != nil {
		t.Fatalf("decode answer : %v", err)
	}
	if len(answer.FormToken) != 32 {
		t.Errorf("FormToken longueur = %d, veut 32", len(answer.FormToken))
	}
	if store.ByToken(answer.FormToken) == nil {
		t.Error("transaction non stockee apres CreatePayment")
	}
}

func TestCreatePaymentPersistsDomainState(t *testing.T) {
	t.Parallel()
	server, store := newTestServer(t)

	body := CreatePaymentRequest{OrderID: "o-1", Amount: 2000, Currency: "EUR"}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment", body, "u", "p")

	var answer CreatePaymentAnswer
	_ = json.Unmarshal(resp.Answer, &answer)

	tx := store.ByToken(answer.FormToken)
	if tx == nil {
		t.Fatal("transaction absente")
	}
	if tx.Payment == nil {
		t.Fatal("Payment nil")
	}
	if got := tx.Payment.State(); string(got) != "initiated" {
		t.Errorf("State = %q, veut initiated", got)
	}
	if tx.Payment.Amount() != 2000 {
		t.Errorf("Amount = %d, veut 2000", tx.Payment.Amount())
	}
}

func TestCreatePaymentUUIDIsV4(t *testing.T) {
	t.Parallel()
	server, store := newTestServer(t)

	body := CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment", body, "u", "p")
	var answer CreatePaymentAnswer
	_ = json.Unmarshal(resp.Answer, &answer)

	tx := store.ByToken(answer.FormToken)
	if tx == nil {
		t.Fatal("transaction absente")
	}
	// UUID v4 : 36 caracteres, 4 tirets, quatrieme groupe commence par
	// un chiffre parmi 8/9/a/b (variant), troisieme groupe commence
	// par 4 (version).
	if len(tx.UUID) != 36 || strings.Count(tx.UUID, "-") != 4 {
		t.Errorf("UUID = %q, format invalide", tx.UUID)
	}
	parts := strings.Split(tx.UUID, "-")
	if len(parts[2]) != 4 || parts[2][0] != '4' {
		t.Errorf("version = %q, veut commencer par 4", parts[2])
	}
	if len(parts[3]) != 4 || !strings.ContainsRune("89ab", rune(parts[3][0])) {
		t.Errorf("variant = %q, veut commencer par 8/9/a/b", parts[3])
	}
}

func TestCreatePaymentRejectsInvalidAmount(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	body := CreatePaymentRequest{OrderID: "o", Amount: 0, Currency: "EUR"}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment", body, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidAmount {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidAmount)
	}
}

func TestCreatePaymentRejectsInvalidCurrency(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	body := CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "euro"}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment", body, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidCurrency {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidCurrency)
	}
}

func TestCreatePaymentRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api-payment/V4/Charge/CreatePayment",
		strings.NewReader("{not json"))
	req.SetBasicAuth("u", "p")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body APIResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Status != "ERROR" {
		t.Errorf("Status = %q, veut ERROR", body.Status)
	}
}

func TestBasicAuthMissing(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api-payment/V4/Charge/CreatePayment",
		strings.NewReader(`{"orderId":"o","amount":100,"currency":"EUR"}`))
	req.Header.Set("Content-Type", "application/json")
	// Pas de SetBasicAuth.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, veut 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic") {
		t.Errorf("WWW-Authenticate = %q, veut prefixe Basic", got)
	}
}

func TestBasicAuthEmpty(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	// User et pass explicitement vides — refus.
	_, status := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "", "")
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, veut 401", status)
	}
}

func TestTransactionGetKnown(t *testing.T) {
	t.Parallel()
	server, store := newTestServer(t)

	// Preparer une transaction en passant par le handler CreatePayment.
	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o-1", Amount: 500, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)
	tx := store.ByToken(ca.FormToken)
	if tx == nil {
		t.Fatal("transaction absente apres CreatePayment")
	}

	get, _ := post(t, server.URL+"/api-payment/V4/Transaction/Get",
		TransactionGetRequest{UUID: tx.UUID}, "u", "p")

	if get.Status != "SUCCESS" {
		t.Fatalf("Status = %q, veut SUCCESS", get.Status)
	}
	var a TransactionGetAnswer
	if err := json.Unmarshal(get.Answer, &a); err != nil {
		t.Fatalf("decode answer : %v", err)
	}
	if a.OrderID != "o-1" || a.Amount != 500 || a.Currency != "EUR" {
		t.Errorf("answer = %+v", a)
	}
	if string(a.OrderStatus) != "initiated" {
		t.Errorf("OrderStatus = %q, veut initiated", a.OrderStatus)
	}
}

func TestTransactionGetUnknown(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	resp, _ := post(t, server.URL+"/api-payment/V4/Transaction/Get",
		TransactionGetRequest{UUID: "inexistant"}, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeUUIDUnknown {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeUUIDUnknown)
	}
}

func TestTransactionGetMissingUUID(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	resp, _ := post(t, server.URL+"/api-payment/V4/Transaction/Get",
		TransactionGetRequest{UUID: ""}, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidRequest {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidRequest)
	}
}

func TestUpdatePaymentUpdatesCustomer(t *testing.T) {
	t.Parallel()
	server, store := newTestServer(t)

	// Créer d'abord une transaction.
	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// La mettre à jour avec un customer.
	update, _ := post(t, server.URL+"/api-payment/V4/Charge/UpdatePayment",
		UpdatePaymentRequest{
			FormToken: ca.FormToken,
			Customer:  Customer{Email: "test@example.com"},
		}, "u", "p")
	if update.Status != "SUCCESS" {
		t.Fatalf("Status = %q, veut SUCCESS", update.Status)
	}

	tx := store.ByToken(ca.FormToken)
	if tx == nil || tx.Customer.Email != "test@example.com" {
		t.Errorf("Customer.Email = %v, veut test@example.com", tx)
	}
}

func TestUpdatePaymentRejectsUnknownToken(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/UpdatePayment",
		UpdatePaymentRequest{FormToken: "inexistant"}, "u", "p")
	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeTokenUnknown {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeTokenUnknown)
	}
}

func TestUpdatePaymentRejectsMissingToken(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/UpdatePayment",
		UpdatePaymentRequest{FormToken: ""}, "u", "p")
	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidRequest {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidRequest)
	}
}

func TestCreateSubscriptionSuccess(t *testing.T) {
	t.Parallel()
	server, store := newTestServer(t)

	body := CreateSubscriptionRequest{
		OrderID:            "sub-order-1",
		Amount:             999,
		Currency:           "EUR",
		PaymentMethodToken: "pmt-abc",
		EffectDate:         "2026-08-01T00:00:00Z",
		Rrule:              "RRULE:FREQ=MONTHLY;INTERVAL=1",
	}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription", body, "u", "p")

	if resp.Status != "SUCCESS" {
		t.Fatalf("Status = %q, veut SUCCESS", resp.Status)
	}
	var a CreateSubscriptionAnswer
	_ = json.Unmarshal(resp.Answer, &a)
	if a.SubscriptionID == "" {
		t.Error("SubscriptionID vide")
	}
	if store.SubscriptionByID(a.SubscriptionID) == nil {
		t.Error("abonnement non stocke apres CreateSubscription")
	}
}

func TestCreateSubscriptionRejectsInvalidAmount(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	body := CreateSubscriptionRequest{
		Amount: 0, Currency: "EUR", PaymentMethodToken: "pmt",
	}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription", body, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidAmount {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidAmount)
	}
}

func TestCreateSubscriptionRejectsInvalidCurrency(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	body := CreateSubscriptionRequest{
		Amount: 100, Currency: "xxx", PaymentMethodToken: "pmt",
	}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription", body, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidCurrency {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidCurrency)
	}
}

func TestCreateSubscriptionRejectsMissingPaymentMethodToken(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	body := CreateSubscriptionRequest{
		Amount: 100, Currency: "EUR", PaymentMethodToken: "",
	}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription", body, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidRequest {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidRequest)
	}
}

func TestSubscriptionGetKnown(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription",
		CreateSubscriptionRequest{
			Amount: 500, Currency: "EUR", PaymentMethodToken: "pmt-xyz",
			EffectDate: "2026-09-01T00:00:00Z",
			Rrule:      "RRULE:FREQ=MONTHLY;INTERVAL=1",
		}, "u", "p")
	var ca CreateSubscriptionAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	get, _ := post(t, server.URL+"/api-payment/V4/Subscription/Get",
		SubscriptionGetRequest{SubscriptionID: ca.SubscriptionID}, "u", "p")

	if get.Status != "SUCCESS" {
		t.Fatalf("Status = %q, veut SUCCESS", get.Status)
	}
	var a SubscriptionGetAnswer
	_ = json.Unmarshal(get.Answer, &a)
	if a.SubscriptionID != ca.SubscriptionID {
		t.Errorf("SubscriptionID = %q", a.SubscriptionID)
	}
	if a.Amount != 500 || a.Currency != "EUR" || a.PaymentMethodToken != "pmt-xyz" {
		t.Errorf("answer = %+v", a)
	}
}

func TestSubscriptionGetUnknown(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	resp, _ := post(t, server.URL+"/api-payment/V4/Subscription/Get",
		SubscriptionGetRequest{SubscriptionID: "inconnu"}, "u", "p")
	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeSubscriptionUnknown {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeSubscriptionUnknown)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	// GET sur un endpoint declare en POST : ServeMux moderne renvoie 405.
	req, _ := http.NewRequest(http.MethodGet,
		server.URL+"/api-payment/V4/Charge/CreatePayment", nil)
	req.SetBasicAuth("u", "p")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, veut 405", resp.StatusCode)
	}
}
