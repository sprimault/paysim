// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
)

// makeTx construit une transaction en etat initiated pour les tests.
func makeTx(t *testing.T, amount format.Amount) *Transaction {
	t.Helper()
	p, err := domain.New("uuid-test", amount, "EUR")
	if err != nil {
		t.Fatalf("domain.New : %v", err)
	}
	return &Transaction{
		FormToken: "tok-test",
		UUID:      "uuid-test",
		OrderID:   "order-42",
		Amount:    amount,
		Currency:  "EUR",
		Payment:   p,
	}
}

func TestApplyOutcomeAll(t *testing.T) {
	t.Parallel()
	cases := []struct {
		outcome   string
		wantState string
	}{
		{OutcomePaid, "captured"},
		{OutcomeAuthorised, "authorized"},
		{OutcomeUnpaid, "declined"},
		{OutcomeExpired, "expired"},
		{OutcomeAbandoned, "expired"}, // mapping documente
	}
	for _, c := range cases {
		t.Run(c.outcome, func(t *testing.T) {
			t.Parallel()
			tx := makeTx(t, 1500)
			if err := applyOutcome(tx, c.outcome, "test"); err != nil {
				t.Fatalf("applyOutcome(%s) : %v", c.outcome, err)
			}
			if got := string(tx.Payment.State()); got != c.wantState {
				t.Errorf("State = %q, veut %q", got, c.wantState)
			}
		})
	}
}

func TestApplyOutcomeUnknown(t *testing.T) {
	t.Parallel()
	tx := makeTx(t, 1500)
	err := applyOutcome(tx, "N_IMPORTE_QUOI", "")
	if !errors.Is(err, ErrUnknownOutcome) {
		t.Errorf("erreur = %v, veut ErrUnknownOutcome", err)
	}
}

func TestApplyOutcomeUnpaidDefaultReason(t *testing.T) {
	t.Parallel()
	// reason vide → "simulation" par defaut.
	tx := makeTx(t, 1500)
	if err := applyOutcome(tx, OutcomeUnpaid, ""); err != nil {
		t.Fatal(err)
	}
	events := tx.Payment.Events()
	last := events[len(events)-1]
	if last.Note != "simulation" {
		t.Errorf("Note = %q, veut simulation", last.Note)
	}
}

func TestBuildKrAnswerPAID(t *testing.T) {
	t.Parallel()
	tx := makeTx(t, 1500)
	_ = applyOutcome(tx, OutcomePaid, "")

	opts := BrowserReturnOpts{Outcome: OutcomePaid}
	answer := buildKrAnswer(tx, opts, "http://paysim", "TEST")

	if answer.OrderStatus != "PAID" {
		t.Errorf("OrderStatus = %q", answer.OrderStatus)
	}
	if answer.OrderCycle != "CLOSED" {
		t.Errorf("OrderCycle = %q, veut CLOSED", answer.OrderCycle)
	}
	if answer.Mode != "TEST" {
		t.Errorf("Mode = %q", answer.Mode)
	}
	if answer.Type != "V4/Payment" {
		t.Errorf("Type = %q", answer.Type)
	}
	if answer.OrderDetails.OrderEffectiveAmount != 1500 {
		t.Errorf("OrderEffectiveAmount = %d, veut 1500 (montant capture)", answer.OrderDetails.OrderEffectiveAmount)
	}
	if len(answer.Transactions) != 1 {
		t.Fatalf("Transactions len = %d, veut 1", len(answer.Transactions))
	}
	tr := answer.Transactions[0]
	if tr.Status != "PAID" || tr.DetailedStatus != "CAPTURED" {
		t.Errorf("Status/DetailedStatus = %q/%q, veut PAID/CAPTURED", tr.Status, tr.DetailedStatus)
	}
	if tr.PaymentMethodType != "CARDS" {
		t.Errorf("PaymentMethodType defaut = %q, veut CARDS", tr.PaymentMethodType)
	}
	if tr.TransactionDetails.CardDetails == nil {
		t.Error("CardDetails nil pour CARDS")
	} else if tr.TransactionDetails.CardDetails.Brand != "VISA" {
		t.Errorf("CardDetails.Brand = %q, veut VISA (defaut)", tr.TransactionDetails.CardDetails.Brand)
	}
	if tr.TransactionDetails.ThreeDSResponse == nil {
		t.Error("ThreeDSResponse nil")
	} else if tr.TransactionDetails.ThreeDSResponse.AuthenticationResultData.Status != "SUCCESS" {
		t.Errorf("3DS status defaut = %q, veut SUCCESS", tr.TransactionDetails.ThreeDSResponse.AuthenticationResultData.Status)
	}
}

func TestBuildKrAnswerAuthorised(t *testing.T) {
	t.Parallel()
	tx := makeTx(t, 1500)
	_ = applyOutcome(tx, OutcomeAuthorised, "")

	opts := BrowserReturnOpts{Outcome: OutcomeAuthorised}
	answer := buildKrAnswer(tx, opts, "", "TEST")

	if answer.OrderCycle != "OPEN" {
		t.Errorf("OrderCycle AUTHORISED = %q, veut OPEN", answer.OrderCycle)
	}
	if answer.OrderDetails.OrderEffectiveAmount != 0 {
		t.Errorf("OrderEffectiveAmount = %d, veut 0 (fonds non captures)", answer.OrderDetails.OrderEffectiveAmount)
	}
	if answer.Transactions[0].DetailedStatus != "AUTHORISED" {
		t.Errorf("DetailedStatus = %q, veut AUTHORISED", answer.Transactions[0].DetailedStatus)
	}
}

func TestBuildKrAnswerUnpaidCarriesError(t *testing.T) {
	t.Parallel()
	tx := makeTx(t, 1500)
	_ = applyOutcome(tx, OutcomeUnpaid, "carte refusée")

	opts := BrowserReturnOpts{
		Outcome:      OutcomeUnpaid,
		ErrorCode:    "PSP_010",
		ErrorMessage: "carte refusée",
	}
	answer := buildKrAnswer(tx, opts, "", "TEST")

	tr := answer.Transactions[0]
	if tr.Status != "UNPAID" || tr.DetailedStatus != "REFUSED" {
		t.Errorf("Status/DetailedStatus = %q/%q", tr.Status, tr.DetailedStatus)
	}
	if tr.ErrorCode != "PSP_010" || tr.ErrorMessage != "carte refusée" {
		t.Errorf("Error propagation = %q / %q", tr.ErrorCode, tr.ErrorMessage)
	}
}

func TestBuildKrAnswerWithWallet(t *testing.T) {
	t.Parallel()
	tx := makeTx(t, 1500)
	_ = applyOutcome(tx, OutcomePaid, "")

	opts := BrowserReturnOpts{
		Outcome: OutcomePaid,
		Wallet:  "APPLE_PAY",
	}
	answer := buildKrAnswer(tx, opts, "", "TEST")
	if answer.Transactions[0].TransactionDetails.Wallet != "APPLE_PAY" {
		t.Errorf("Wallet = %q", answer.Transactions[0].TransactionDetails.Wallet)
	}
}

func TestBuildKrAnswerNonCardsMethodOmitsCardDetails(t *testing.T) {
	t.Parallel()
	tx := makeTx(t, 1500)
	_ = applyOutcome(tx, OutcomePaid, "")

	opts := BrowserReturnOpts{Outcome: OutcomePaid, PaymentMethodType: "IP_WIRE"}
	answer := buildKrAnswer(tx, opts, "", "TEST")

	if answer.Transactions[0].TransactionDetails.CardDetails != nil {
		t.Error("CardDetails devrait etre nil pour IP_WIRE")
	}
}

func TestBuildDeliveryWebhookSignsCorrectly(t *testing.T) {
	t.Parallel()
	tx := makeTx(t, 1500)
	_ = applyOutcome(tx, OutcomePaid, "")

	opts := BrowserReturnOpts{Outcome: OutcomePaid}
	answer := buildKrAnswer(tx, opts, "", "TEST")

	const key = "clef-de-test-hmac"
	wh, hash, err := buildDeliveryWebhook("delivery-1", "http://marchand", answer, key, "V4/Payment")
	if err != nil {
		t.Fatal(err)
	}

	// Le body est du form-urlencoded. Extraire kr-answer et kr-hash.
	values, err := url.ParseQuery(string(wh.Body))
	if err != nil {
		t.Fatalf("body non parsable en form : %v", err)
	}

	krAnswer := values.Get("kr-answer")
	krHash := values.Get("kr-hash")
	if krAnswer == "" || krHash == "" {
		t.Fatal("kr-answer ou kr-hash absent du body")
	}
	if values.Get("kr-hash-algorithm") != "sha256_hmac" {
		t.Errorf("kr-hash-algorithm = %q", values.Get("kr-hash-algorithm"))
	}
	if values.Get("kr-hash-key") != "sha256_hmac" {
		t.Errorf("kr-hash-key = %q", values.Get("kr-hash-key"))
	}
	if values.Get("kr-answer-type") != "V4/Payment" {
		t.Errorf("kr-answer-type = %q", values.Get("kr-answer-type"))
	}

	// Le hash retourne doit correspondre a celui du body.
	if krHash != hash {
		t.Errorf("hash retourne %q != hash body %q", hash, krHash)
	}
	// Verifier avec notre implementation Sign : croise le contrat de sortie.
	if !Verify([]byte(krAnswer), krHash, key) {
		t.Error("Verify sur le kr-answer produit refuse le kr-hash — les deux ne sont pas coherents")
	}

	// Le kr-answer doit etre du JSON valide de forme KrAnswer.
	var back KrAnswer
	if err := json.Unmarshal([]byte(krAnswer), &back); err != nil {
		t.Fatalf("kr-answer non re-decodable : %v", err)
	}
	if back.OrderStatus != "PAID" {
		t.Errorf("OrderStatus apres round-trip = %q", back.OrderStatus)
	}

	// Header Content-Type doit etre form-urlencoded.
	if got := wh.Headers["Content-Type"]; got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", got)
	}
	// URL cible correcte.
	if wh.URL != "http://marchand" {
		t.Errorf("URL = %q", wh.URL)
	}
	// Sanity check : le body encode contient "orderStatus" quelque part.
	if !strings.Contains(string(wh.Body), "orderStatus") {
		t.Error("body ne contient pas orderStatus")
	}
}
