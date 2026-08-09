// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/chaos"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
)

// newTestServer construit un serveur Paysim avec queue interne, config
// vide (Basic Auth permissive, pas de HMAC, pas de bearer). Suffit
// pour les tests des endpoints REST V4 qui n'utilisent pas la queue.
func newTestServer(t *testing.T) (*httptest.Server, Store) {
	t.Helper()
	server, store, _ := newTestServerFull(t, HandlerConfig{})
	return server, store
}

// newTestServerFull expose aussi la queue delivery, utile pour les
// tests d'endpoints de simulation qui verifient le POST sortant. Le
// worker de la queue est lance en background et arrete par le cleanup.
func newTestServerFull(t *testing.T, cfg HandlerConfig) (*httptest.Server, Store, *delivery.Queue) {
	t.Helper()
	store := newMemStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = queue.Run(ctx)
	}()

	server := httptest.NewServer(NewHandler(store, queue, logger, cfg).Routes())
	t.Cleanup(func() {
		server.Close()
		cancel()
		wg.Wait()
	})
	return server, store, queue
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
	if tx, _ := store.ByToken(answer.FormToken); tx == nil {
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

	tx, _ := store.ByToken(answer.FormToken)
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

	tx, _ := store.ByToken(answer.FormToken)
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

	// Montant nul valide depuis le fix REGISTER pur — seul le négatif
	// est rejeté.
	body := CreatePaymentRequest{OrderID: "o", Amount: -100, Currency: "EUR"}
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

// TestCreatePaymentAcceptsZeroAmount vérifie qu'un enrôlement pur
// (formAction REGISTER, amount 0) n'est pas rejeté — c'est le
// comportement d'un vrai PSP qui traite REGISTER sans débit.
func TestCreatePaymentAcceptsZeroAmount(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	body := CreatePaymentRequest{OrderID: "o", Amount: 0, Currency: "EUR", FormAction: "REGISTER"}
	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment", body, "u", "p")

	if resp.Status != "SUCCESS" {
		t.Fatalf("Status = %q, veut SUCCESS (Answer=%s)", resp.Status, resp.Answer)
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
	tx, _ := store.ByToken(ca.FormToken)
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

	tx, _ := store.ByToken(ca.FormToken)
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
	if sub, _ := store.SubscriptionByID(a.SubscriptionID); sub == nil {
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
		SubscriptionGetRequest{
			SubscriptionID: ca.SubscriptionID, PaymentMethodToken: "pmt-xyz",
		}, "u", "p")

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
		SubscriptionGetRequest{
			SubscriptionID: "inconnu", PaymentMethodToken: "pmt-xyz",
		}, "u", "p")
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

// -----------------------------------------------------------------------------
// Tests des endpoints de simulation (/paysim/simulate/*)
// -----------------------------------------------------------------------------

// receivedWebhook capture le POST recu par un httptest.Server jouant
// le role du marchand. Le canal recoit une entree par POST recu.
type receivedWebhook struct {
	Body    []byte
	Headers http.Header
	Values  url.Values
}

// newMerchantServer construit un httptest.Server qui capture chaque
// POST recu et le renvoie sur un canal, avec HTTP 200 par defaut.
// Utile pour tester la livraison des webhooks depuis Paysim.
func newMerchantServer(t *testing.T) (*httptest.Server, <-chan receivedWebhook) {
	t.Helper()
	ch := make(chan receivedWebhook, 5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		ch <- receivedWebhook{Body: body, Headers: r.Header.Clone(), Values: values}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, ch
}

// simulate envoie un POST vers un endpoint de simulation Paysim et
// decode la reponse. Boilerplate factorise.
func simulate(t *testing.T, url string, body any, bearer string) (*BrowserReturnResponse, int) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out BrowserReturnResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return &out, resp.StatusCode
}

func TestBrowserReturnSuccess(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "test-hmac-key"})
	merchant, received := newMerchantServer(t)

	// Créer une transaction via CreatePayment.
	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o-1", Amount: 1500, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Déclencher la simulation.
	resp, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, veut 200", status)
	}
	if resp.Status != "SUCCESS" || resp.DeliveryID == "" || resp.KrHash == "" {
		t.Errorf("réponse incomplète : %+v", resp)
	}

	// Attendre le webhook côté marchand.
	select {
	case wh := <-received:
		if wh.Values.Get("kr-answer") == "" {
			t.Error("kr-answer absent")
		}
		if wh.Values.Get("kr-hash") != resp.KrHash {
			t.Errorf("kr-hash mismatch : reçu %q, retourné %q",
				wh.Values.Get("kr-hash"), resp.KrHash)
		}
		if wh.Values.Get("kr-hash-algorithm") != "sha256_hmac" {
			t.Errorf("kr-hash-algorithm = %q", wh.Values.Get("kr-hash-algorithm"))
		}
		if wh.Headers.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", wh.Headers.Get("Content-Type"))
		}
		// Le kr-answer doit contenir orderStatus=PAID.
		if !strings.Contains(wh.Values.Get("kr-answer"), `"orderStatus":"PAID"`) {
			t.Errorf("kr-answer ne contient pas orderStatus PAID : %s",
				wh.Values.Get("kr-answer"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook non reçu après 2s")
	}
}

func TestBrowserReturnUsesTransactionReturnURL(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, received := newMerchantServer(t)

	// ReturnURL stockée à CreatePayment.
	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{
			OrderID: "o", Amount: 100, Currency: "EUR",
			ReturnURL: merchant.URL,
		}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Pas de ReturnURL dans la simulation → fallback sur celle stockée.
	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{FormToken: ca.FormToken, Outcome: OutcomePaid}, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook non reçu")
	}
}

func TestBrowserReturnPrioritySimulationOverTransaction(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	transactionURL, _ := newMerchantServer(t)
	simulationURL, simReceived := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{
			OrderID: "o", Amount: 100, Currency: "EUR",
			ReturnURL: transactionURL.URL,
		}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// URL de simulation ≠ URL de transaction → la simulation gagne.
	_, _ = simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: simulationURL.URL,
			Outcome:   OutcomePaid,
		}, "")

	select {
	case <-simReceived:
		// Bon serveur.
	case <-time.After(2 * time.Second):
		t.Fatal("webhook non reçu par le serveur de simulation")
	}
}

func TestBrowserReturnMissingURL(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Ni ReturnURL dans la simulation, ni dans la transaction.
	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{FormToken: ca.FormToken, Outcome: OutcomePaid}, "")
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400", status)
	}
}

// TestBrowserReturnFallbackDefaultCallbackURL vérifie que la
// simulation utilise HandlerConfig.DefaultCallbackURL quand ni la
// requête ni la transaction ne fournissent d'URL — c'est le
// comportement que le simulateur doit avoir en mode dev pour ne pas
// obliger le marchand à câbler returnUrl à chaque paiement.
func TestBrowserReturnFallbackDefaultCallbackURL(t *testing.T) {
	t.Parallel()
	merchant, hits := newMerchantServer(t)
	server, _, _ := newTestServerFull(t, HandlerConfig{
		HMACKey:            "k",
		DefaultCallbackURL: merchant.URL,
	})

	// createPayment sans ReturnURL — la transaction ne l'aura pas.
	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Simulate sans ReturnURL non plus — doit tomber sur le fallback.
	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{FormToken: ca.FormToken, Outcome: OutcomePaid}, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, veut 200 (fallback config utilisé)", status)
	}
	// Le marchand doit avoir reçu la livraison.
	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("aucune livraison au marchand — le fallback DefaultCallbackURL n'a pas déclenché")
	}
}

func TestBrowserReturnMissingHMAC(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{}) // pas de HMACKey
	merchant, _ := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "")
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400 (HMAC manquant)", status)
	}
}

func TestBrowserReturnUnknownOutcome(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, _ := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   "N_IMPORTE_QUOI",
		}, "")
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400", status)
	}
}

func TestBrowserReturnUnknownFormToken(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, _ := newMerchantServer(t)

	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: "inexistant",
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "")
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400", status)
	}
}

func TestBrowserReturnDomainConflict(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, _ := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Premier appel PAID → captured.
	_, _ = simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "")

	// Deuxième appel PAID → transition interdite (déjà captured).
	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "")
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400 (transition interdite)", status)
	}
}

func TestIPNSuccess(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, received := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{
			OrderID: "o", Amount: 100, Currency: "EUR",
			NotificationURL: merchant.URL,
		}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	_, status := simulate(t, server.URL+"/paysim/simulate/ipn",
		IPNRequest{FormToken: ca.FormToken, Outcome: OutcomePaid}, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}

	select {
	case wh := <-received:
		if wh.Values.Get("kr-hash") == "" {
			t.Error("kr-hash absent")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook IPN non reçu")
	}
}

func TestBearerAuthMissing(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k", APIToken: "secret-bearer"})

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/simulate/browserReturn",
		strings.NewReader(`{"formToken":"x","outcome":"PAID"}`))
	req.Header.Set("Content-Type", "application/json")
	// Pas de Bearer.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, veut 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
		t.Errorf("WWW-Authenticate = %q", got)
	}
}

func TestBearerAuthWrongToken(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k", APIToken: "secret-bearer"})

	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{FormToken: "x", Outcome: OutcomePaid}, "mauvais-token")
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, veut 401", status)
	}
}

func TestBearerAuthCorrectToken(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k", APIToken: "secret-bearer"})
	merchant, _ := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Bon bearer → passe.
	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "secret-bearer")
	if status != http.StatusOK {
		t.Errorf("status = %d, veut 200", status)
	}
}

// -----------------------------------------------------------------------------
// Tests chaos (vertical 1 phase 2)
// -----------------------------------------------------------------------------

func TestChaosInjectsErrorOnAPIRoutes(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := chaos.New(chaos.Config{ErrorRate: 100}, logger)

	server, _, _ := newTestServerFull(t, HandlerConfig{Chaos: c})

	// /api-payment/V4/* passe par le middleware chaos → 500 systématique.
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api-payment/V4/Charge/CreatePayment",
		strings.NewReader(`{"orderId":"o","amount":100,"currency":"EUR"}`))
	req.SetBasicAuth("u", "p")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, veut 500 (chaos injecté)", resp.StatusCode)
	}
}

func TestChaosDoesNotAffectSimulateRoutes(t *testing.T) {
	t.Parallel()
	// ErrorRate=100 sur /api-payment/* NE DOIT PAS impacter /paysim/simulate/*.
	// C'est le contrat : chaos simule les défauts PSP, pas ceux de l'API
	// de contrôle Paysim.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := chaos.New(chaos.Config{ErrorRate: 100}, logger)

	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k", Chaos: c})

	// L'appel simulate avec un formToken inconnu doit répondre 400
	// (erreur métier), pas 500 (chaos). Preuve que le middleware chaos
	// n'est pas sur cette route.
	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{FormToken: "inexistant", Outcome: OutcomePaid, ReturnURL: "http://x"}, "")
	if status == http.StatusInternalServerError {
		t.Errorf("simulate reçoit 500 alors que chaos ne doit pas s'y appliquer")
	}
}

func TestMagicAmountForcesUnpaid(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, received := newMerchantServer(t)

	// Montant se terminant par 01 → force UNPAID quel que soit outcome demandé.
	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 1501, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// On demande PAID, mais magic amount doit forcer UNPAID.
	_, _ = simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "")

	select {
	case wh := <-received:
		if !strings.Contains(wh.Values.Get("kr-answer"), `"orderStatus":"UNPAID"`) {
			t.Errorf("orderStatus attendu UNPAID (magic 01), reçu : %s",
				wh.Values.Get("kr-answer"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook non reçu")
	}
}

func TestMagicAmountLatencyOnCreatePayment(t *testing.T) {
	t.Parallel()
	// Montant se terminant par 03 → latence 30s côté serveur. On la
	// coupe via un contexte client à 200ms pour valider que la latence
	// est bien appliquée (le client abandonne avant que le serveur ne
	// réponde). Impossible sans magic value dans un délai normal.
	server, _, _ := newTestServerFull(t, HandlerConfig{})

	body := `{"orderId":"o","amount":1503,"currency":"EUR"}`
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api-payment/V4/Charge/CreatePayment",
		strings.NewReader(body))
	req.SetBasicAuth("u", "p")
	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)

	// L'appel doit être coupé par le timeout client, pas répondre.
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("appel a répondu en %v, veut timeout (magic latence 03 = 30s)", elapsed)
	}
	// Le timeout doit avoir tenu au moins 190ms (la latence de 30s a
	// bien démarré).
	if elapsed < 190*time.Millisecond {
		t.Errorf("timeout à %v, veut >= 190ms", elapsed)
	}
}

func TestChaosDefaultInertOnAPIRoutes(t *testing.T) {
	t.Parallel()
	// HandlerConfig sans Chaos → aucun middleware chaos, invariant 5.
	server, _, _ := newTestServerFull(t, HandlerConfig{})

	// Un CreatePayment simple ne doit pas voir d'injection 500 ni de
	// latence anormale.
	start := time.Now()
	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("appel a duré %v sans chaos configuré, veut instantané", elapsed)
	}
	if create.Status != "SUCCESS" {
		t.Errorf("status = %q sans chaos, veut SUCCESS", create.Status)
	}
}

// -----------------------------------------------------------------------------
// Tests chaos sur webhooks (vertical 3 phase 2)
// -----------------------------------------------------------------------------

func TestWebhookChaosDuplicate(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, received := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	_, _ = simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
			Chaos:     WebhookChaos{Duplicate: true},
		}, "")

	// Attendre les 2 POSTs.
	got := 0
	timeout := time.After(2 * time.Second)
	for got < 2 {
		select {
		case <-received:
			got++
		case <-timeout:
			t.Fatalf("attendu 2 webhooks (duplicate), reçus %d", got)
		}
	}
}

func TestWebhookChaosBadSignature(t *testing.T) {
	t.Parallel()
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "cle-hmac"})
	merchant, received := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	resp, _ := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
			Chaos:     WebhookChaos{BadSignature: true},
		}, "")

	select {
	case wh := <-received:
		sentHash := wh.Values.Get("kr-hash")
		// Le hash envoyé doit être différent du hash retourné à l'appelant
		// (qui reste le vrai hash pour diagnostic).
		if sentHash == resp.KrHash {
			t.Errorf("BadSignature actif mais hash envoyé == hash annoncé (%q)", sentHash)
		}
		// Le hash marchand recalculé ne doit PAS matcher — c'est le contrat.
		krAnswer := wh.Values.Get("kr-answer")
		if Verify([]byte(krAnswer), sentHash, "cle-hmac") {
			t.Error("Verify du marchand accepte le hash altéré — le chaos badSignature ne fonctionne pas")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook non reçu")
	}
}

func TestWebhookChaosOutOfOrder(t *testing.T) {
	t.Parallel()
	// Composition : deux appels successifs, le premier avec DeliveryDelayMs=300,
	// le second sans → le second arrive en premier chez le marchand.
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})

	// Marchand qui tag chaque POST par ordre d'arrivée dans un channel.
	arrivals := make(chan string, 2)
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		krAnswer := values.Get("kr-answer")
		// UNPAID contient PAID en sous-chaîne — tester UNPAID en
		// premier, ou utiliser la forme JSON complète.
		switch {
		case strings.Contains(krAnswer, `"orderStatus":"UNPAID"`):
			arrivals <- "UNPAID"
		case strings.Contains(krAnswer, `"orderStatus":"PAID"`):
			arrivals <- "PAID"
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer merchant.Close()

	// Deux transactions distinctes.
	create1, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "premier", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca1 CreatePaymentAnswer
	_ = json.Unmarshal(create1.Answer, &ca1)
	create2, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "second", Amount: 200, Currency: "EUR"}, "u", "p")
	var ca2 CreatePaymentAnswer
	_ = json.Unmarshal(create2.Answer, &ca2)

	// Simuler dans l'ordre : premier PAID avec délai 300ms, puis second UNPAID immédiat.
	_, _ = simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca1.FormToken, ReturnURL: merchant.URL,
			Outcome: OutcomePaid, DeliveryDelayMs: 300,
		}, "")
	_, _ = simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca2.FormToken, ReturnURL: merchant.URL,
			Outcome: OutcomeUnpaid,
		}, "")

	first := <-arrivals
	second := <-arrivals
	if first != "UNPAID" || second != "PAID" {
		t.Errorf("ordre reçu [%s, %s], veut [UNPAID, PAID] (out-of-order par delay)", first, second)
	}
}

func TestWebhookChaosRaceBeforeResponse(t *testing.T) {
	t.Parallel()
	// La réponse HTTP à simulate doit arriver APRÈS le webhook côté
	// marchand. Vérifie que la course la plus subtile est bien reproduite.
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})

	webhookAt := make(chan time.Time, 1)
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookAt <- time.Now()
		w.WriteHeader(http.StatusOK)
	}))
	defer merchant.Close()

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Mesurer l'instant de la réponse HTTP à browserReturn.
	body, _ := json.Marshal(BrowserReturnRequest{
		FormToken: ca.FormToken,
		ReturnURL: merchant.URL,
		Outcome:   OutcomePaid,
		Chaos:     WebhookChaos{RaceBeforeResponse: true},
	})
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/simulate/browserReturn",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	responseAt := time.Now()
	_ = resp.Body.Close()

	select {
	case whAt := <-webhookAt:
		if !whAt.Before(responseAt) {
			t.Errorf("webhook reçu à %v, réponse HTTP à %v : la course n'a pas eu lieu",
				whAt, responseAt)
		}
		// La différence doit être significative (le sleep est de 500ms côté serveur).
		if diff := responseAt.Sub(whAt); diff < 100*time.Millisecond {
			t.Errorf("écart webhook→réponse = %v, veut >= 100ms (race pas assez marquée)", diff)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook non reçu")
	}
}

func TestBearerOpenWhenTokenUnset(t *testing.T) {
	t.Parallel()
	// APIToken vide → pas de check, tout passe.
	server, _, _ := newTestServerFull(t, HandlerConfig{HMACKey: "k"})
	merchant, _ := newMerchantServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{OrderID: "o", Amount: 100, Currency: "EUR"}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	// Aucun bearer, ça doit passer.
	_, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{
			FormToken: ca.FormToken,
			ReturnURL: merchant.URL,
			Outcome:   OutcomePaid,
		}, "")
	if status != http.StatusOK {
		t.Errorf("status = %d, veut 200", status)
	}
}

// TestReplayFallbackDefaultCallbackURL couvre le rejeu one-click sans
// notificationUrl. Un paiement recurrent est declenche par un
// ordonnanceur : personne n'est la pour fournir une URL, et exiger
// qu'elle soit explicite revenait a n'emettre aucune notification.
func TestReplayFallbackDefaultCallbackURL(t *testing.T) {
	t.Parallel()
	merchant, hits := newMerchantServer(t)
	cfg := HandlerConfig{HMACKey: "k", DefaultCallbackURL: merchant.URL}
	_, store, queue := newTestServerFull(t, cfg)

	pm := NewPaymentMethod("tok-replay", Card{
		PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030, Brand: "VISA",
	}, Customer{}, time.Now().UTC())
	if err := store.SaveMethod(pm); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
	// Aucune NotificationURL : tout repose sur le repli.
	if _, err := h.Create(CreateInput{
		Amount: 4990, Currency: "EUR", OrderID: "CHARGE-1",
		PaymentMethodToken: "tok-replay",
	}); err != nil {
		t.Fatalf("rejeu: %v", err)
	}

	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("aucune notification sur un rejeu sans notificationUrl — le repli n'a pas joue")
	}
}

// TestTriggerBillingNotifie couvre l'echeance d'abonnement, qui
// n'emettait aucun webhook. C'est le seul chemin ou le marchand ne peut
// rien apprendre autrement : sans notification, une reprise d'impaye est
// intestable de bout en bout.
func TestTriggerBillingNotifie(t *testing.T) {
	t.Parallel()
	merchant, hits := newMerchantServer(t)
	cfg := HandlerConfig{HMACKey: "k", DefaultCallbackURL: merchant.URL}
	_, store, queue := newTestServerFull(t, cfg)

	pm := NewPaymentMethod("tok-sub", Card{
		PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030, Brand: "VISA",
	}, Customer{}, time.Now().UTC())
	if err := store.SaveMethod(pm); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
	sub, err := h.CreateSubscription(CreateSubscriptionInput{
		PaymentMethodToken: "tok-sub", Amount: 1990, Currency: "EUR", OrderID: "SUB-1",
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if _, err := h.TriggerBilling(sub.ID); err != nil {
		t.Fatalf("trigger billing: %v", err)
	}

	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("aucune notification sur une echeance d abonnement")
	}
}

// TestAutoplayJoueLePaiementEtNotifie : sans autoplay un paiement reste
// initiated tant que personne ne l'a joue. Avec, il est capture et
// notifie des la creation — ce que le porteur aurait declenche.
func TestAutoplayJoueLePaiementEtNotifie(t *testing.T) {
	t.Parallel()
	merchant, hits := newMerchantServer(t)
	cfg := HandlerConfig{HMACKey: "k", DefaultCallbackURL: merchant.URL, Autoplay: true}
	_, store, queue := newTestServerFull(t, cfg)
	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)

	tx, err := h.Create(CreateInput{Amount: 4990, Currency: "EUR", OrderID: "AUTO-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := string(tx.Payment.State()); got != "captured" {
		t.Errorf("state = %q, veut captured", got)
	}
	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("aucune notification alors que l autoplay est actif")
	}
}

// TestAutoplayDesactiveParDefaut verrouille l'invariant : un paiement
// neuf ne bouge pas tant que personne ne l'a joue.
func TestAutoplayDesactiveParDefaut(t *testing.T) {
	t.Parallel()
	merchant, _ := newMerchantServer(t)
	cfg := HandlerConfig{HMACKey: "k", DefaultCallbackURL: merchant.URL}
	_, store, queue := newTestServerFull(t, cfg)
	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)

	tx, err := h.Create(CreateInput{Amount: 4990, Currency: "EUR", OrderID: "MANUEL-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := string(tx.Payment.State()); got != "initiated" {
		t.Errorf("state = %q, veut initiated — l autoplay ne doit pas etre actif par defaut", got)
	}
}

// TestAutoplayRespecteLesValeursMagiques est le test qui compte : le
// mode automatise qui joue, pas ce qui sort. Si l'issue etait forcee a
// PAID, les quatre leviers de testing-cards deviendraient inoperants
// des l'activation du flag.
func TestAutoplayRespecteLesValeursMagiques(t *testing.T) {
	t.Parallel()
	cases := []struct {
		nom       string
		amount    format.Amount
		card      *Card
		wantState string
	}{
		{"montant magique refuse", 1001, nil, "declined"},
		{"montant normal capture", 1000, nil, "captured"},
		{"PAN de refus", 2000, &Card{
			PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2030,
		}, "declined"},
		{"carte expiree", 2000, &Card{
			PAN: "4111111111111111", ExpiryMonth: 1, ExpiryYear: 2020,
		}, "declined"},
		{"carte valide", 2000, &Card{
			PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030,
		}, "captured"},
	}
	for _, c := range cases {
		t.Run(c.nom, func(t *testing.T) {
			t.Parallel()
			merchant, _ := newMerchantServer(t)
			cfg := HandlerConfig{HMACKey: "k", DefaultCallbackURL: merchant.URL, Autoplay: true}
			_, store, queue := newTestServerFull(t, cfg)
			h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)

			tx, err := h.Create(CreateInput{
				Amount: c.amount, Currency: "EUR", OrderID: "MAGIC", Card: c.card,
			})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if got := string(tx.Payment.State()); got != c.wantState {
				t.Errorf("state = %q, veut %q", got, c.wantState)
			}
		})
	}
}

// TestCustomerReferenceRemonteDansKrAnswer : reference identifie le
// client cote marchand et permet de rapprocher un paiement d'un compte
// sans passer par la metadata. Absente de la struct, elle etait ecartee
// au decodage JSON sans erreur — le marchand l'envoyait et ne la
// retrouvait jamais.
func TestCustomerReferenceRemonteDansKrAnswer(t *testing.T) {
	t.Parallel()
	merchant, hits := newMerchantServer(t)
	server, _, _ := newTestServerFull(t, HandlerConfig{
		HMACKey: "k", DefaultCallbackURL: merchant.URL,
	})

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreatePayment",
		CreatePaymentRequest{
			OrderID: "o", Amount: 100, Currency: "EUR",
			Customer: Customer{
				Reference: "demo-org",
				Email:     "compta@demo-org.fr",
			},
		}, "u", "p")
	var ca CreatePaymentAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	if _, status := simulate(t, server.URL+"/paysim/simulate/browserReturn",
		BrowserReturnRequest{FormToken: ca.FormToken, Outcome: OutcomePaid}, ""); status != http.StatusOK {
		t.Fatalf("simulate status = %d", status)
	}

	select {
	case got := <-hits:
		var answer KrAnswer
		if err := json.Unmarshal([]byte(got.Values.Get("kr-answer")), &answer); err != nil {
			t.Fatalf("kr-answer illisible : %v", err)
		}
		if answer.Customer.Reference != "demo-org" {
			t.Errorf("customer.reference = %q, veut demo-org", answer.Customer.Reference)
		}
		// L'email doit rester intact : reference s'ajoute, ne remplace pas.
		if answer.Customer.Email != "compta@demo-org.fr" {
			t.Errorf("customer.email = %q", answer.Customer.Email)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aucune livraison au marchand")
	}
}

// L'alias doit porter le client de l'enrôlement : c'est le préalable à
// tout le reste, puisque c'est lui qui fera autorité au rejeu.
func TestEnrolementCaptureLeClient(t *testing.T) {
	t.Parallel()
	cfg := HandlerConfig{HMACKey: "k"}
	_, store, queue := newTestServerFull(t, cfg)
	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)

	tx, err := h.Create(CreateInput{
		Amount: 0, Currency: "EUR", OrderID: "ENROL", FormAction: "REGISTER",
		Customer: Customer{
			Reference: "client-A", Email: "a@example.com",
			BillingDetails: BillingDetails{LastName: "MARTIN", Country: "FR"},
		},
		Card: &Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	if err != nil {
		t.Fatal(err)
	}

	pm, err := store.MethodByToken(tx.PaymentMethodToken)
	if err != nil || pm == nil {
		t.Fatalf("moyen introuvable : %v", err)
	}
	if pm.Customer.Reference != "client-A" || pm.Customer.Email != "a@example.com" {
		t.Errorf("client de l'alias = %+v, veut client-A/a@example.com", pm.Customer)
	}
	if pm.Customer.BillingDetails.LastName != "MARTIN" {
		t.Errorf("billingDetails non capture : %+v", pm.Customer.BillingDetails)
	}
}

// Le comportement que corrige ce changement : au rejeu, un customer
// divergent est ignoré au profit de celui de l'alias. Un marchand qui se
// trompe de référence ne le verrait pas chez PayZen ; Paysim ne doit pas
// le lui montrer davantage, sous peine de valider en test une
// intégration qui dérive en production.
func TestRejeuIgnoreLeClientDeLaRequete(t *testing.T) {
	t.Parallel()
	cfg := HandlerConfig{HMACKey: "k"}
	_, store, queue := newTestServerFull(t, cfg)
	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)

	enrol, err := h.Create(CreateInput{
		Amount: 0, Currency: "EUR", OrderID: "ENROL", FormAction: "REGISTER",
		Customer: Customer{
			Reference: "client-A", Email: "a@example.com",
			BillingDetails: BillingDetails{LastName: "MARTIN"},
		},
		Card: &Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Rejeu avec un tout autre client, et une livraison propre à cette
	// commande.
	rejeu, err := h.Create(CreateInput{
		Amount: 1990, Currency: "EUR", OrderID: "REPLAY",
		PaymentMethodToken: enrol.PaymentMethodToken,
		Customer: Customer{
			Reference: "client-B", Email: "b@example.com",
			BillingDetails:  BillingDetails{LastName: "DURAND"},
			ShippingDetails: ShippingDetails{City: "Lyon", ShippingMethod: "RELAY_POINT"},
			ExtraDetails:    ExtraDetails{IPAddress: "203.0.113.9"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// L'alias fait foi sur l'identité.
	if rejeu.Customer.Reference != "client-A" {
		t.Errorf("reference = %q, veut client-A (celle de l'alias)", rejeu.Customer.Reference)
	}
	if rejeu.Customer.Email != "a@example.com" {
		t.Errorf("email = %q, veut celui de l'alias", rejeu.Customer.Email)
	}
	if rejeu.Customer.BillingDetails.LastName != "MARTIN" {
		t.Errorf("billingDetails = %+v, veut celui de l'alias", rejeu.Customer.BillingDetails)
	}

	// Mais la livraison et le contexte navigateur appartiennent à cette
	// commande-ci : PayZen ne prétend pas les écraser.
	if rejeu.Customer.ShippingDetails.City != "Lyon" {
		t.Errorf("shippingDetails ecrase a tort : %+v", rejeu.Customer.ShippingDetails)
	}
	if rejeu.Customer.ExtraDetails.IPAddress != "203.0.113.9" {
		t.Errorf("extraDetails ecrase a tort : %+v", rejeu.Customer.ExtraDetails)
	}
}

// Un alias enrôlé avant que le client soit capturé n'en porte aucun : le
// rejeu retombe alors sur celui de la requête, faute de mieux.
func TestRejeuSurAliasSansClientGardeLaRequete(t *testing.T) {
	t.Parallel()
	cfg := HandlerConfig{HMACKey: "k"}
	_, store, queue := newTestServerFull(t, cfg)

	pm := NewPaymentMethod("tok-ancien", Card{
		PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2030,
	}, Customer{}, time.Now().UTC())
	if err := store.SaveMethod(pm); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
	tx, err := h.Create(CreateInput{
		Amount: 1000, Currency: "EUR", OrderID: "REPLAY-OLD",
		PaymentMethodToken: "tok-ancien",
		Customer:           Customer{Reference: "client-B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Customer.Reference != "client-B" {
		t.Errorf("reference = %q, veut client-B (l'alias n'en porte pas)", tx.Customer.Reference)
	}
}

// Le token est requis, comme chez PayZen. L'accepter absent rendrait
// Paysim plus permissif que le vrai : l'appel passerait ici et échouerait
// en production, sans que rien ne l'ait signalé.
func TestSubscriptionGetSansTokenRefuse(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription",
		CreateSubscriptionRequest{
			Amount: 500, Currency: "EUR", PaymentMethodToken: "pmt-abc",
			EffectDate: "2026-09-01T00:00:00Z",
			Rrule:      "RRULE:FREQ=MONTHLY;INTERVAL=1",
		}, "u", "p")
	var ca CreateSubscriptionAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	resp, _ := post(t, server.URL+"/api-payment/V4/Subscription/Get",
		SubscriptionGetRequest{SubscriptionID: ca.SubscriptionID}, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR sans paymentMethodToken", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeInvalidRequest {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodeInvalidRequest)
	}
}

// Un token qui ne correspond pas à l'abonnement est traité comme un
// abonnement inconnu : ne pas distinguer les deux cas évite de renseigner
// un appelant sur l'existence d'un abonnement dont il ignore le moyen.
func TestSubscriptionGetTokenIncoherentRefuse(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	create, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription",
		CreateSubscriptionRequest{
			Amount: 500, Currency: "EUR", PaymentMethodToken: "pmt-vrai",
			EffectDate: "2026-09-01T00:00:00Z",
			Rrule:      "RRULE:FREQ=MONTHLY;INTERVAL=1",
		}, "u", "p")
	var ca CreateSubscriptionAnswer
	_ = json.Unmarshal(create.Answer, &ca)

	resp, _ := post(t, server.URL+"/api-payment/V4/Subscription/Get",
		SubscriptionGetRequest{
			SubscriptionID: ca.SubscriptionID, PaymentMethodToken: "pmt-autre",
		}, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR sur un couple incoherent", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodeSubscriptionUnknown {
		t.Errorf("ErrorCode = %q, veut %q — meme reponse qu'un abonnement inconnu",
			e.ErrorCode, ErrCodeSubscriptionUnknown)
	}
}

// Le motif doit accompagner le refus dans le kr-answer : c'est lui que
// le marchand lit pour décider s'il retente ou s'il réclame une autre
// carte. Sans lui, la logique de reconduction s'écrit à l'aveugle.
func TestMotifDeRefusDansLeKrAnswer(t *testing.T) {
	t.Parallel()
	cfg := HandlerConfig{HMACKey: "k"}
	_, store, queue := newTestServerFull(t, cfg)
	h := NewHandler(store, queue, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)

	cases := []struct {
		nom      string
		pan      string
		wantCode string
	}{
		{"provision insuffisante", "4000000000000002", "51"},
		{"carte volee", "5105105105105100", "43"},
		{"refus generique", "2223000000000007", "05"},
		{"operation non permise", "378282000000008", "57"},
	}
	for _, c := range cases {
		t.Run(c.nom, func(t *testing.T) {
			enrol, err := h.Create(CreateInput{
				Amount: 0, Currency: "EUR", OrderID: "ENROL", FormAction: "REGISTER",
				Card: &Card{PAN: c.pan, ExpiryMonth: 12, ExpiryYear: 2030},
			})
			if err != nil {
				t.Fatal(err)
			}
			tx, err := h.Create(CreateInput{
				Amount: 2500, Currency: "EUR", OrderID: "REJEU",
				PaymentMethodToken: enrol.PaymentMethodToken,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := string(tx.Payment.State()); got != "declined" {
				t.Fatalf("state = %q, veut declined", got)
			}

			pm, _ := store.MethodByToken(enrol.PaymentMethodToken)
			answer := buildKrAnswer(tx, pm, BrowserReturnOpts{
				Outcome:       OutcomeUnpaid,
				DeclineReason: chaos.DeclineReasonForPAN(c.pan),
			}, "", "TEST")
			if len(answer.Transactions) == 0 {
				t.Fatal("aucune transaction dans le kr-answer")
			}
			if got := answer.Transactions[0].DetailedErrorCode; got != c.wantCode {
				t.Errorf("detailedErrorCode = %q, veut %q", got, c.wantCode)
			}
			if answer.Transactions[0].DetailedErrorMessage == "" {
				t.Error("detailedErrorMessage vide — le code seul n'est pas lisible")
			}
		})
	}
}

// Le montant magique est le levier du parcours utilisateur ; le PAN
// celui du récurrent, où le montant est imposé par l'abonnement.
func TestMotifDeRefusParMontantMagique(t *testing.T) {
	t.Parallel()
	cases := map[format.Amount]string{
		1001: "51",
		1002: "43",
		1004: "91",
	}
	for amount, want := range cases {
		answer := buildKrAnswer(
			&Transaction{
				UUID: "u", OrderID: "O", Amount: amount, Currency: "EUR",
				Payment: newDeclinedPayment(t, amount),
			},
			nil,
			BrowserReturnOpts{
				Outcome:       OutcomeUnpaid,
				DeclineReason: chaos.MagicDeclineReason(amount),
			}, "", "TEST")
		if got := answer.Transactions[0].DetailedErrorCode; got != want {
			t.Errorf("montant %d : detailedErrorCode = %q, veut %q", amount, got, want)
		}
	}
}

// Un succès ne porte aucun motif : le champ doit rester absent du JSON,
// pas valoir une chaîne vide.
func TestSuccesSansMotifDeRefus(t *testing.T) {
	t.Parallel()
	tx := &Transaction{
		UUID: "u", OrderID: "O", Amount: 1000, Currency: "EUR",
		Payment: newCapturedPayment(t),
	}
	answer := buildKrAnswer(tx, nil, BrowserReturnOpts{Outcome: OutcomePaid}, "", "TEST")
	if got := answer.Transactions[0].DetailedErrorCode; got != "" {
		t.Errorf("detailedErrorCode = %q sur un succes, veut vide", got)
	}
}

func newDeclinedPayment(t *testing.T, amount format.Amount) *domain.Payment {
	t.Helper()
	p, err := domain.New("u", amount, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Decline("test"); err != nil {
		t.Fatal(err)
	}
	return p
}

func newCapturedPayment(t *testing.T) *domain.Payment {
	t.Helper()
	p, err := domain.New("u", 1000, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Capture(); err != nil {
		t.Fatal(err)
	}
	return p
}

// Un refus doit porter un code PSP en plus du motif bancaire : le
// protocole en promet un, et sans lui le marchand devrait deviner qu'il
// s'agit d'un refus à partir du seul statut.
func TestRefusPorteLeCodePSP(t *testing.T) {
	t.Parallel()
	tx := &Transaction{
		UUID: "u", OrderID: "O", Amount: 1001, Currency: "EUR",
		Payment: newDeclinedPayment(t, 1001),
	}
	answer := buildKrAnswer(tx, nil, BrowserReturnOpts{
		Outcome:       OutcomeUnpaid,
		DeclineReason: chaos.ReasonInsufficientFunds,
	}, "", "TEST")

	got := answer.Transactions[0]
	if got.ErrorCode != ErrCodeRefused {
		t.Errorf("errorCode = %q, veut %q", got.ErrorCode, ErrCodeRefused)
	}
	if got.DetailedErrorCode != "51" {
		t.Errorf("detailedErrorCode = %q, veut 51", got.DetailedErrorCode)
	}
}
