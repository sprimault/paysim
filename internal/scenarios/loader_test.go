// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_validMinimal(t *testing.T) {
	t.Parallel()

	const yml = `
name: hello
steps:
  - action: assert_state
    state: initiated
`
	got, err := Load(strings.NewReader(yml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "hello" {
		t.Errorf("Name = %q, veut %q", got.Name, "hello")
	}
	if len(got.Steps) != 1 {
		t.Fatalf("nb etapes = %d, veut 1", len(got.Steps))
	}
	step := got.Steps[0]
	if step.Action != ActionAssertState {
		t.Errorf("Action = %q, veut %q", step.Action, ActionAssertState)
	}
	if step.AssertState == nil {
		t.Fatalf("AssertState nil")
	}
	if step.AssertState.State != "initiated" {
		t.Errorf("State = %q, veut %q", step.AssertState.State, "initiated")
	}
}

func TestLoad_allActionsFromFile(t *testing.T) {
	t.Parallel()

	s, err := LoadFile(filepath.Join("testdata", "nominal.yml"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	wantActions := []string{
		ActionCreatePayment,
		ActionSimulate,
		ActionWait,
		ActionAssertWebhook,
		ActionAssertState,
	}
	if len(s.Steps) != len(wantActions) {
		t.Fatalf("nb etapes = %d, veut %d", len(s.Steps), len(wantActions))
	}
	for i, want := range wantActions {
		if s.Steps[i].Action != want {
			t.Errorf("etape %d action = %q, veut %q", i+1, s.Steps[i].Action, want)
		}
	}

	// Vérifie que le dispatch a bien peuplé le bon pointeur pour chaque étape.
	if cp := s.Steps[0].CreatePayment; cp == nil {
		t.Fatalf("CreatePayment nil")
	} else if cp.Provider != "payzen" || cp.Amount != 1000 || cp.Currency != "EUR" || cp.OrderID != "ORDER-001" {
		t.Errorf("CreatePayment = %+v", cp)
	}
	if w := s.Steps[2].Wait; w == nil {
		t.Fatalf("Wait nil")
	} else if time.Duration(w.Duration) != 100*time.Millisecond {
		t.Errorf("Wait.Duration = %v, veut 100ms", time.Duration(w.Duration))
	}
	if aw := s.Steps[3].AssertWebhook; aw == nil {
		t.Fatalf("AssertWebhook nil")
	} else if aw.Count != 1 || aw.Status != "PAID" {
		t.Errorf("AssertWebhook = %+v", aw)
	}
}

func TestLoad_injectAndWait(t *testing.T) {
	t.Parallel()

	const yml = `
name: chaos
steps:
  - action: inject
    mode: duplicate
  - action: wait
    duration: 250ms
`
	got, err := Load(strings.NewReader(yml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Steps[0].Inject == nil || got.Steps[0].Inject.Mode != "duplicate" {
		t.Errorf("Inject = %+v", got.Steps[0].Inject)
	}
	if got.Steps[1].Wait == nil || time.Duration(got.Steps[1].Wait.Duration) != 250*time.Millisecond {
		t.Errorf("Wait = %+v", got.Steps[1].Wait)
	}
}

func TestLoad_errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantSub string // sous-chaîne attendue dans le message d'erreur
	}{
		{
			name: "nom manquant",
			yaml: `
steps:
  - action: assert_state
    state: initiated
`,
			wantSub: "scenario sans nom",
		},
		{
			name: "aucune etape",
			yaml: `
name: vide
steps: []
`,
			wantSub: "scenario sans etape",
		},
		{
			name: "action inconnue",
			yaml: `
name: x
steps:
  - action: teleport
    target: mars
`,
			wantSub: "action inconnue",
		},
		{
			name: "action absente",
			yaml: `
name: x
steps:
  - provider: payzen
`,
			wantSub: "action absent",
		},
		{
			name: "create_payment sans provider",
			yaml: `
name: x
steps:
  - action: create_payment
    amount: 1000
    currency: EUR
    order_id: ORDER-001
`,
			wantSub: "provider vide",
		},
		{
			name: "create_payment montant negatif",
			yaml: `
name: x
steps:
  - action: create_payment
    provider: payzen
    amount: -10
    currency: EUR
    order_id: ORDER-001
`,
			wantSub: "amount doit etre positif ou nul",
		},
		{
			name: "wait duration nulle",
			yaml: `
name: x
steps:
  - action: wait
    duration: 0s
`,
			wantSub: "duration doit etre strictement positive",
		},
		{
			name: "wait duration invalide",
			yaml: `
name: x
steps:
  - action: wait
    duration: soon
`,
			wantSub: "duration",
		},
		{
			name: "simulate sans status",
			yaml: `
name: x
steps:
  - action: simulate
`,
			wantSub: "status vide",
		},
		{
			name: "assert_webhook count negatif",
			yaml: `
name: x
steps:
  - action: assert_webhook
    count: -1
`,
			wantSub: "count doit etre positif",
		},
		{
			name: "yaml malforme",
			yaml: `name: x
steps: - not a list
`,
			wantSub: "decodage yaml",
		},
		{
			name: "card avec expiry_month invalide",
			yaml: `
name: x
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O
    card:
      pan: "4111111111111111"
      expiry_month: 13
      expiry_year: 2028
`,
			wantSub: "expiry_month = 13",
		},
		{
			name: "card avec pan vide",
			yaml: `
name: x
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O
    card:
      pan: ""
      expiry_month: 12
      expiry_year: 2028
`,
			wantSub: "pan vide",
		},
		{
			name: "charge_token sans amount",
			yaml: `
name: x
steps:
  - action: charge_token
    currency: EUR
    order_id: O
`,
			wantSub: "amount doit etre strictement positif",
		},
		{
			name: "create_subscription sans currency",
			yaml: `
name: x
steps:
  - action: create_subscription
    amount: 1000
    order_id: O
`,
			wantSub: "currency vide",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(strings.NewReader(c.yaml))
			if err == nil {
				t.Fatalf("Load a reussi, attendait une erreur contenant %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("erreur = %q, veut contenir %q", err.Error(), c.wantSub)
			}
		})
	}
}

func TestLoad_agregeLesErreurs(t *testing.T) {
	t.Parallel()

	const yml = `
steps:
  - action: create_payment
    amount: -10
`
	_, err := Load(strings.NewReader(yml))
	if err == nil {
		t.Fatalf("attendait une erreur")
	}
	msg := err.Error()
	for _, sub := range []string{"scenario sans nom", "provider vide", "amount doit", "currency vide", "order_id vide"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("message d'erreur ne contient pas %q\nobtenu: %s", sub, msg)
		}
	}
}

func TestLoadFile_subscriptionScenario(t *testing.T) {
	t.Parallel()
	s, err := LoadFile(filepath.Join("testdata", "subscription.yml"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(s.Steps) != 9 {
		t.Fatalf("nb etapes = %d, veut 9", len(s.Steps))
	}
	// Étape 2 = create_subscription avec metadata.
	cs := s.Steps[1].CreateSubscription
	if cs == nil {
		t.Fatalf("create_subscription absent en etape 2")
	}
	if cs.Amount != 2990 || cs.Rrule != "RRULE:FREQ=MONTHLY;INTERVAL=1" {
		t.Errorf("create_subscription = %+v", cs)
	}
	if cs.Metadata["plan"] != "pro" {
		t.Errorf("Metadata[plan] = %q, veut pro", cs.Metadata["plan"])
	}
	// Étape 3 = assert_subscription cancelled=false (pointeur *bool
	// distingue « non fourni » de « false »).
	as := s.Steps[2].AssertSubscription
	if as == nil || as.Cancelled == nil {
		t.Fatalf("assert_subscription.Cancelled nil")
	}
	if *as.Cancelled != false {
		t.Errorf("Cancelled = %v, veut false", *as.Cancelled)
	}
	// Étape 8 = cancel_subscription (payload vide légitime).
	if s.Steps[7].CancelSubscription == nil {
		t.Fatalf("cancel_subscription absent en etape 8")
	}
}

func TestLoadFile_recurringScenario(t *testing.T) {
	t.Parallel()
	s, err := LoadFile(filepath.Join("testdata", "recurring.yml"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(s.Steps) != 5 {
		t.Fatalf("nb etapes = %d, veut 5", len(s.Steps))
	}
	create := s.Steps[0].CreatePayment
	if create == nil || create.Card == nil {
		t.Fatalf("create_payment.card absent")
	}
	if create.Card.PAN != "4111111111111111" {
		t.Errorf("PAN = %q", create.Card.PAN)
	}
	if create.FormAction != "REGISTER_PAY" {
		t.Errorf("FormAction = %q", create.FormAction)
	}
	charge := s.Steps[3].ChargeToken
	if charge == nil {
		t.Fatalf("charge_token absent en etape 4")
	}
	if charge.Amount != 2990 || charge.OrderID != "SUB-42-M2" {
		t.Errorf("charge_token = %+v", charge)
	}
}

func TestLoadFile_canonicalExamples(t *testing.T) {
	t.Parallel()
	// Les scénarios canoniques publiés dans examples/scenarios/ doivent
	// rester valides — si le format YAML évolue, cette suite casse et
	// force la mise à jour cohérente doc + exemples.
	examples := []string{
		"one-shot.yml",
		"one-shot-declined.yml",
		"recurring-token.yml",
		"subscription.yml",
		"subscription-with-decline.yml",
		"chaos-duplicate.yml",
	}
	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("..", "..", "examples", "scenarios", name)
			if _, err := LoadFile(path); err != nil {
				t.Errorf("LoadFile(%s): %v", path, err)
			}
		})
	}
}

func TestLoadFile_notFound(t *testing.T) {
	t.Parallel()
	_, err := LoadFile(filepath.Join("testdata", "does-not-exist.yml"))
	if err == nil {
		t.Fatalf("attendait une erreur")
	}
	if !strings.Contains(err.Error(), "ouverture") {
		t.Errorf("message = %q, veut contenir %q", err.Error(), "ouverture")
	}
}
