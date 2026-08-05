// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/format"
)

// atState amène un paiement neuf jusqu'à l'état demandé, en enchaînant les
// transitions minimales requises. Utilisé pour construire des Payment dans
// n'importe quel état sans dupliquer la mécanique dans chaque test.
func atState(t *testing.T, s State) *Payment {
	t.Helper()
	p, err := New("pay-1", 10000, "EUR")
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	switch s {
	case StateInitiated:
		// rien à faire
	case StateAuthorized:
		must(t, p.Authorize())
	case StateCaptured:
		must(t, p.Capture())
	case StateRefunded:
		must(t, p.Capture())
		must(t, p.Refund(10000))
	case StatePartiallyRefunded:
		must(t, p.Capture())
		must(t, p.Refund(3000))
	case StateDeclined:
		must(t, p.Decline("raison"))
	case StateExpired:
		must(t, p.Expire())
	case StateChargeback:
		must(t, p.Capture())
		must(t, p.Chargeback())
	default:
		t.Fatalf("état non supporté par atState : %s", s)
	}
	if p.State() != s {
		t.Fatalf("atState(%s) a produit %s", s, p.State())
	}
	return p
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("erreur inattendue : %v", err)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		id       string
		amount   format.Amount
		currency string
		wantErr  error
	}{
		{"nominal", "pay-1", 1000, "EUR", nil},
		{"id vide", "", 1000, "EUR", ErrInvalidPayment},
		// Montant nul valide : cas d'un enrôlement pur (formAction
		// PayZen REGISTER) où on crée une transaction sans débit.
		{"montant nul (REGISTER)", "pay-1", 0, "EUR", nil},
		{"montant negatif", "pay-1", -100, "EUR", ErrInvalidAmount},
		{"devise vide", "pay-1", 1000, "", ErrInvalidCurrency},
		{"devise minuscule", "pay-1", 1000, "eur", ErrInvalidCurrency},
		{"devise trop courte", "pay-1", 1000, "EU", ErrInvalidCurrency},
		{"devise trop longue", "pay-1", 1000, "EURO", ErrInvalidCurrency},
		{"devise chiffres", "pay-1", 1000, "978", ErrInvalidCurrency},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			before := time.Now().UTC()
			p, err := New(c.id, c.amount, c.currency)
			after := time.Now().UTC()

			if !errors.Is(err, c.wantErr) {
				t.Fatalf("erreur %v, veut %v", err, c.wantErr)
			}
			if c.wantErr != nil {
				return
			}
			if p.State() != StateInitiated {
				t.Errorf("état %s, veut initiated", p.State())
			}
			if p.Amount() != c.amount {
				t.Errorf("montant %d, veut %d", p.Amount(), c.amount)
			}
			if p.Refunded() != 0 {
				t.Errorf("cumul remboursé %d, veut 0", p.Refunded())
			}
			ev := p.Events()
			if len(ev) != 1 || ev[0].Kind != EventCreated {
				t.Errorf("journal initial %+v, veut un seul EventCreated", ev)
			}
			if p.CreatedAt() != p.UpdatedAt() {
				t.Errorf("createdAt (%v) != updatedAt (%v) à la création", p.CreatedAt(), p.UpdatedAt())
			}
			if p.CreatedAt().Before(before) || p.CreatedAt().After(after) {
				t.Errorf("createdAt %v hors intervalle [%v, %v]", p.CreatedAt(), before, after)
			}
		})
	}
}

// TestValidTransitions couvre toutes les transitions valides déclenchées par
// une méthode sans paramètre. Refund a son test dédié (TestRefund) parce que
// son résultat dépend d'un paramètre montant.
func TestValidTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from      State
		action    string
		apply     func(*Payment) error
		wantState State
	}{
		{StateInitiated, "Authorize", (*Payment).Authorize, StateAuthorized},
		{StateInitiated, "Capture", (*Payment).Capture, StateCaptured},
		{StateInitiated, "Expire", (*Payment).Expire, StateExpired},
		{StateAuthorized, "Capture", (*Payment).Capture, StateCaptured},
		{StateAuthorized, "Expire", (*Payment).Expire, StateExpired},
		{StateCaptured, "Chargeback", (*Payment).Chargeback, StateChargeback},
		{StateRefunded, "Chargeback", (*Payment).Chargeback, StateChargeback},
		{StatePartiallyRefunded, "Chargeback", (*Payment).Chargeback, StateChargeback},
	}
	// Cas Decline traités séparément — signature différente.
	for _, c := range cases {
		t.Run(string(c.from)+"_"+c.action, func(t *testing.T) {
			t.Parallel()
			p := atState(t, c.from)
			before := time.Now().UTC()
			if err := c.apply(p); err != nil {
				t.Fatalf("%s depuis %s : %v", c.action, c.from, err)
			}
			after := time.Now().UTC()
			if p.State() != c.wantState {
				t.Errorf("état %s, veut %s", p.State(), c.wantState)
			}
			last := p.Events()[len(p.Events())-1]
			if last.At.Before(before) || last.At.After(after) {
				t.Errorf("timestamp événement %v hors intervalle", last.At)
			}
		})
	}
}

// TestDeclineFromInitiatedAndAuthorized couvre Decline séparément parce que
// sa signature diffère (paramètre reason) et qu'on veut vérifier que la
// raison est bien portée au journal.
func TestDeclineFromInitiatedAndAuthorized(t *testing.T) {
	t.Parallel()
	for _, from := range []State{StateInitiated, StateAuthorized} {
		t.Run(string(from), func(t *testing.T) {
			t.Parallel()
			p := atState(t, from)
			if err := p.Decline("fonds insuffisants"); err != nil {
				t.Fatalf("Decline depuis %s : %v", from, err)
			}
			if p.State() != StateDeclined {
				t.Errorf("état %s, veut declined", p.State())
			}
			last := p.Events()[len(p.Events())-1]
			if last.Kind != EventDeclined || last.Note != "fonds insuffisants" {
				t.Errorf("dernier événement %+v", last)
			}
		})
	}
}

// TestRefund couvre les cas fonctionnels du remboursement : partiel, cumul,
// atteinte du total, dépassement, montant invalide.
func TestRefund(t *testing.T) {
	t.Parallel()

	t.Run("total en une fois", func(t *testing.T) {
		t.Parallel()
		p := atState(t, StateCaptured)
		if err := p.Refund(10000); err != nil {
			t.Fatalf("Refund : %v", err)
		}
		if p.State() != StateRefunded {
			t.Errorf("état %s, veut refunded", p.State())
		}
		if p.Refunded() != 10000 {
			t.Errorf("cumul %d, veut 10000", p.Refunded())
		}
	})

	t.Run("plusieurs partiels puis complet", func(t *testing.T) {
		t.Parallel()
		p := atState(t, StateCaptured)
		must(t, p.Refund(3000))
		if p.State() != StatePartiallyRefunded {
			t.Fatalf("après 3000/10000 : état %s", p.State())
		}
		must(t, p.Refund(5000))
		if p.State() != StatePartiallyRefunded {
			t.Fatalf("après 8000/10000 : état %s", p.State())
		}
		if p.Refunded() != 8000 {
			t.Errorf("cumul %d, veut 8000", p.Refunded())
		}
		must(t, p.Refund(2000))
		if p.State() != StateRefunded {
			t.Errorf("après cumul atteint : état %s, veut refunded", p.State())
		}
	})

	t.Run("depassement", func(t *testing.T) {
		t.Parallel()
		p := atState(t, StateCaptured)
		if err := p.Refund(15000); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("erreur %v, veut ErrInvalidAmount", err)
		}
		if p.State() != StateCaptured {
			t.Errorf("état changé malgré erreur : %s", p.State())
		}
		if p.Refunded() != 0 {
			t.Errorf("cumul %d, veut 0 (erreur ne doit rien laisser)", p.Refunded())
		}
	})

	t.Run("cumul depasse", func(t *testing.T) {
		t.Parallel()
		p := atState(t, StateCaptured)
		must(t, p.Refund(7000))
		if err := p.Refund(5000); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("erreur %v, veut ErrInvalidAmount", err)
		}
		if p.State() != StatePartiallyRefunded || p.Refunded() != 7000 {
			t.Errorf("état/cumul modifiés après erreur : %s/%d", p.State(), p.Refunded())
		}
	})

	t.Run("montant nul", func(t *testing.T) {
		t.Parallel()
		p := atState(t, StateCaptured)
		if err := p.Refund(0); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("erreur %v, veut ErrInvalidAmount", err)
		}
	})

	t.Run("montant negatif", func(t *testing.T) {
		t.Parallel()
		p := atState(t, StateCaptured)
		if err := p.Refund(-100); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("erreur %v, veut ErrInvalidAmount", err)
		}
	})
}

// TestInvalidTransitions couvre exhaustivement toutes les combinaisons
// (état source, action) qui doivent renvoyer ErrInvalidTransition. C'est
// le miroir de TestValidTransitions : les cases non listées ici sont valides
// et l'inverse. La table est explicite pour qu'une régression saute aux yeux.
func TestInvalidTransitions(t *testing.T) {
	t.Parallel()
	// Ensemble des actions applicables. Une action retourne l'erreur reçue.
	actions := map[string]func(*Payment) error{
		"Authorize":  (*Payment).Authorize,
		"Capture":    (*Payment).Capture,
		"Refund":     func(p *Payment) error { return p.Refund(1000) },
		"Decline":    func(p *Payment) error { return p.Decline("test") },
		"Expire":     (*Payment).Expire,
		"Chargeback": (*Payment).Chargeback,
	}
	// Actions interdites depuis chaque état. Le complémentaire (dans le sens
	// mathématique) est l'ensemble des actions valides pour cet état.
	forbidden := map[State][]string{
		StateInitiated:         {"Refund", "Chargeback"},
		StateAuthorized:        {"Authorize", "Refund", "Chargeback"},
		StateCaptured:          {"Authorize", "Capture", "Decline", "Expire"},
		StateRefunded:          {"Authorize", "Capture", "Refund", "Decline", "Expire"},
		StatePartiallyRefunded: {"Authorize", "Capture", "Decline", "Expire"},
		StateDeclined:          {"Authorize", "Capture", "Refund", "Decline", "Expire", "Chargeback"},
		StateExpired:           {"Authorize", "Capture", "Refund", "Decline", "Expire", "Chargeback"},
		StateChargeback:        {"Authorize", "Capture", "Refund", "Decline", "Expire", "Chargeback"},
	}
	for state, forbiddenActions := range forbidden {
		for _, name := range forbiddenActions {
			t.Run(string(state)+"_"+name, func(t *testing.T) {
				t.Parallel()
				p := atState(t, state)
				before := p.State()
				beforeEvents := len(p.Events())
				err := actions[name](p)
				if !errors.Is(err, ErrInvalidTransition) {
					t.Errorf("erreur %v, veut ErrInvalidTransition", err)
				}
				if p.State() != before {
					t.Errorf("état changé malgré erreur : %s -> %s", before, p.State())
				}
				if len(p.Events()) != beforeEvents {
					t.Errorf("journal étendu malgré erreur : %d -> %d", beforeEvents, len(p.Events()))
				}
			})
		}
	}
}

// TestEventsImmutable vérifie que la slice retournée par Events est bien une
// copie : la modifier ne doit pas altérer le journal interne.
func TestEventsImmutable(t *testing.T) {
	t.Parallel()
	p := atState(t, StateCaptured)
	events := p.Events()
	events[0] = Event{Kind: EventDeclined, Note: "corrompu"}
	if p.Events()[0].Kind != EventCreated {
		t.Errorf("le journal interne a été altéré via la copie retournée")
	}
}

// TestIsTerminal vérifie que exactement les états attendus sont terminaux.
func TestIsTerminal(t *testing.T) {
	t.Parallel()
	want := map[State]bool{
		StateInitiated:         false,
		StateAuthorized:        false,
		StateCaptured:          false,
		StatePartiallyRefunded: false,
		StateRefunded:          true,
		StateDeclined:          true,
		StateExpired:           true,
		StateChargeback:        true,
	}
	for s, w := range want {
		if got := s.IsTerminal(); got != w {
			t.Errorf("%s.IsTerminal() = %v, veut %v", s, got, w)
		}
	}
}
