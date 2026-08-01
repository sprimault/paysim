// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package scenarios porte le format YAML des scénarios de test et son loader.
// Un scénario est une suite d'étapes typées que l'exécuteur (à venir en 4.4.2)
// applique contre un Paysim distant : création de paiement, avancement dans la
// machine à états, injection de panne, assertions sur les webhooks et l'état
// final. Le loader garantit qu'après Load réussi tout est déjà validé —
// l'exécuteur n'a pas à revérifier la forme, seulement la sémantique métier
// (mode de chaos réellement supporté, état domain existant).
//
// Format retenu : style impératif, une liste d'étapes où chaque map porte un
// discriminant `action:`. La discrimination explicite est plus verbeuse qu'une
// clé de map unique par action, mais s'auto-documente et supporte l'évolution
// (ajout de champs communs, tri d'options).
package scenarios

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Actions supportées, exportées pour être partagées avec l'exécuteur et la
// documentation. Les valeurs sont figées par le format et ne doivent pas
// changer entre versions sans un renommage explicite dans les scénarios
// commités des utilisateurs.
const (
	ActionCreatePayment = "create_payment"
	ActionSimulate      = "simulate"
	ActionInject        = "inject"
	ActionWait          = "wait"
	ActionAssertWebhook = "assert_webhook"
	ActionAssertState   = "assert_state"
)

// Scenario est un scénario complet chargé depuis un fichier YAML. Une fois
// obtenu via Load ou LoadFile, tous ses champs sont conformes au format
// (name non vide, au moins une étape, chaque étape validée).
type Scenario struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Steps       []Step `yaml:"steps"`
}

// Step est une étape typée. Le champ Action porte le discriminant lu depuis
// YAML ; exactement un des pointeurs concrets est non nil, celui qui
// correspond à Action. C'est la responsabilité du loader ; les consommateurs
// peuvent s'appuyer sur cette invariante sans revérifier.
type Step struct {
	Action string

	CreatePayment *CreatePayment
	Simulate      *Simulate
	Inject        *Inject
	Wait          *Wait
	AssertWebhook *AssertWebhook
	AssertState   *AssertState
}

// CreatePayment demande à Paysim de créer un paiement via un provider.
// Amount est en centimes entiers de la devise (invariant Paysim, jamais de
// flottant), OrderID identifie la commande côté marchand.
type CreatePayment struct {
	Provider string `yaml:"provider"`
	Amount   int64  `yaml:"amount"`
	Currency string `yaml:"currency"`
	OrderID  string `yaml:"order_id"`
}

// Simulate avance le paiement dans la machine à états via l'API de simulation
// de Paysim. Status est l'état cible tel que perçu côté API (`captured`,
// `refunded`, `declined`…), pas un statut protocolaire de fournisseur.
type Simulate struct {
	Status string `yaml:"status"`
}

// Inject active un mode de panne du moteur de chaos pour les webhooks émis à
// partir de l'étape suivante. Le vocabulaire de Mode suit celui du paquet
// internal/chaos (`duplicate`, `delay`, `bad-signature`, `race`).
type Inject struct {
	Mode string `yaml:"mode"`
}

// Wait suspend l'exécution du scénario pendant Duration. Utile pour laisser
// la file de livraison drainer un webhook différé avant une assertion.
type Wait struct {
	Duration Duration `yaml:"duration"`
}

// AssertWebhook vérifie qu'un certain nombre de webhooks ont été livrés
// depuis le début du scénario, avec optionnellement un filtre sur le statut
// (vocabulaire natif du fournisseur, par exemple `PAID` pour PayZen). Un
// Status vide compte tous les webhooks du paiement courant.
type AssertWebhook struct {
	Count  int    `yaml:"count"`
	Status string `yaml:"status,omitempty"`
}

// AssertState vérifie que le paiement courant est dans l'état State (nom
// canonique de la machine à états : `initiated`, `authorized`, `captured`,
// `partially_refunded`, `refunded`, `declined`, `expired`, `chargeback`).
type AssertState struct {
	State string `yaml:"state"`
}

// Duration accepte les durées YAML sous forme de chaîne parsée par
// time.ParseDuration ("500ms", "2s", "1m30s"). Un alias plutôt qu'un
// time.Duration nu pour maîtriser le message d'erreur en français.
type Duration time.Duration

// UnmarshalYAML implémente yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration doit etre une chaine")
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("duration %q invalide: %w", node.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

// UnmarshalYAML dispatche le décodage d'une étape selon son champ `action`.
// Le contrat sortant : après retour sans erreur, Action est renseigné et
// exactement un des pointeurs concrets est non nil.
func (s *Step) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("etape doit etre une map")
	}
	var head struct {
		Action string `yaml:"action"`
	}
	if err := node.Decode(&head); err != nil {
		return fmt.Errorf("lecture du champ action: %w", err)
	}
	s.Action = head.Action
	switch head.Action {
	case "":
		return errors.New("champ action absent ou vide")
	case ActionCreatePayment:
		s.CreatePayment = &CreatePayment{}
		return node.Decode(s.CreatePayment)
	case ActionSimulate:
		s.Simulate = &Simulate{}
		return node.Decode(s.Simulate)
	case ActionInject:
		s.Inject = &Inject{}
		return node.Decode(s.Inject)
	case ActionWait:
		s.Wait = &Wait{}
		return node.Decode(s.Wait)
	case ActionAssertWebhook:
		s.AssertWebhook = &AssertWebhook{}
		return node.Decode(s.AssertWebhook)
	case ActionAssertState:
		s.AssertState = &AssertState{}
		return node.Decode(s.AssertState)
	default:
		return fmt.Errorf("action inconnue: %q", head.Action)
	}
}

// Load décode un scénario YAML depuis r et le valide. Retourne toujours un
// scénario prêt à exécuter ou une erreur descriptive citant le champ ou
// l'étape en cause — jamais un scénario partiellement valide.
func Load(r io.Reader) (*Scenario, error) {
	var s Scenario
	if err := yaml.NewDecoder(r).Decode(&s); err != nil {
		return nil, fmt.Errorf("decodage yaml: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// LoadFile est la variante fichier de Load. Les erreurs d'ouverture ou de
// décodage sont enrichies du chemin pour faciliter le diagnostic quand
// plusieurs scénarios sont chargés en batch (CI, tests d'intégration).
func LoadFile(path string) (*Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ouverture %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	s, err := Load(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}
