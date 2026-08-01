// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"errors"
	"fmt"
	"time"
)

// Validate parcourt le scénario et agrège toutes les erreurs de forme. Le
// choix d'agréger plutôt que de sortir à la première erreur : l'auteur d'un
// scénario veut voir toutes les corrections à faire en une passe, pas les
// découvrir une par une à chaque relance.
func (s *Scenario) Validate() error {
	var errs []error
	if s.Name == "" {
		errs = append(errs, errors.New("scenario sans nom"))
	}
	if len(s.Steps) == 0 {
		errs = append(errs, errors.New("scenario sans etape"))
	}
	for i, step := range s.Steps {
		if err := step.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("etape %d (%s): %w", i+1, step.Action, err))
		}
	}
	return errors.Join(errs...)
}

// Validate vérifie que le pointeur concret correspondant à Action est bien
// présent et que ses champs sont conformes. La correspondance Action /
// pointeur non nil est déjà garantie par le loader ; on la revérifie
// néanmoins pour ne pas exploser sur un Scenario construit à la main dans
// les tests.
func (s Step) Validate() error {
	switch s.Action {
	case ActionCreatePayment:
		if s.CreatePayment == nil {
			return errors.New("payload create_payment manquant")
		}
		return s.CreatePayment.Validate()
	case ActionSimulate:
		if s.Simulate == nil {
			return errors.New("payload simulate manquant")
		}
		return s.Simulate.Validate()
	case ActionInject:
		if s.Inject == nil {
			return errors.New("payload inject manquant")
		}
		return s.Inject.Validate()
	case ActionWait:
		if s.Wait == nil {
			return errors.New("payload wait manquant")
		}
		return s.Wait.Validate()
	case ActionAssertWebhook:
		if s.AssertWebhook == nil {
			return errors.New("payload assert_webhook manquant")
		}
		return s.AssertWebhook.Validate()
	case ActionAssertState:
		if s.AssertState == nil {
			return errors.New("payload assert_state manquant")
		}
		return s.AssertState.Validate()
	default:
		return fmt.Errorf("action inconnue: %q", s.Action)
	}
}

// Validate contrôle la forme d'un CreatePayment. Aucune connaissance du
// domaine ni des providers ici : les valeurs concrètes (provider existant,
// devise ISO 4217) sont validées à l'exécution.
func (c *CreatePayment) Validate() error {
	var errs []error
	if c.Provider == "" {
		errs = append(errs, errors.New("provider vide"))
	}
	if c.Amount <= 0 {
		errs = append(errs, errors.New("amount doit etre strictement positif"))
	}
	if c.Currency == "" {
		errs = append(errs, errors.New("currency vide"))
	}
	if c.OrderID == "" {
		errs = append(errs, errors.New("order_id vide"))
	}
	return errors.Join(errs...)
}

// Validate contrôle qu'un Simulate porte bien un status. Le vocabulaire est
// délégué à l'exécuteur — le loader n'a pas à connaître la liste des états.
func (s *Simulate) Validate() error {
	if s.Status == "" {
		return errors.New("status vide")
	}
	return nil
}

// Validate contrôle qu'un Inject porte bien un mode.
func (i *Inject) Validate() error {
	if i.Mode == "" {
		return errors.New("mode vide")
	}
	return nil
}

// Validate contrôle qu'un Wait a une durée strictement positive. Autoriser
// zéro n'apporte rien et masquerait une durée oubliée (`duration:` vide dans
// le YAML, valeur zéro par défaut).
func (w *Wait) Validate() error {
	if time.Duration(w.Duration) <= 0 {
		return errors.New("duration doit etre strictement positive")
	}
	return nil
}

// Validate contrôle qu'un AssertWebhook porte un count positif ou nul.
// Status vide est légitime — cela signifie « n'importe quel statut ».
func (a *AssertWebhook) Validate() error {
	if a.Count < 0 {
		return errors.New("count doit etre positif ou nul")
	}
	return nil
}

// Validate contrôle qu'un AssertState porte bien un state. Le vocabulaire
// est délégué à l'exécuteur qui connaît la machine à états.
func (a *AssertState) Validate() error {
	if a.State == "" {
		return errors.New("state vide")
	}
	return nil
}
