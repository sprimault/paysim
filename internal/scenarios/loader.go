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
	ActionCreatePayment      = "create_payment"
	ActionSimulate           = "simulate"
	ActionInject             = "inject"
	ActionWait               = "wait"
	ActionAssertWebhook      = "assert_webhook"
	ActionAssertState        = "assert_state"
	ActionChargeToken        = "charge_token"
	ActionCreateSubscription = "create_subscription"
	ActionTriggerBilling     = "trigger_billing"
	ActionAssertSubscription = "assert_subscription"
	ActionCancelSubscription = "cancel_subscription"
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

	CreatePayment      *CreatePayment
	Simulate           *Simulate
	Inject             *Inject
	Wait               *Wait
	AssertWebhook      *AssertWebhook
	AssertState        *AssertState
	ChargeToken        *ChargeToken
	CreateSubscription *CreateSubscription
	TriggerBilling     *TriggerBilling
	AssertSubscription *AssertSubscription
	CancelSubscription *CancelSubscription
}

// CreatePayment demande à Paysim de créer un paiement via un provider.
// Amount est en centimes entiers de la devise (invariant Paysim, jamais de
// flottant), OrderID identifie la commande côté marchand.
//
// Champs 4.4.5 (paiements récurrents) :
//   - Card : si présent, Paysim enregistre le moyen de paiement à la
//     création et retourne un paymentMethodToken réutilisable par
//     charge_token. Cet enrôlement est **systématique** dès qu'une Card
//     est fournie, indépendamment de FormAction — cf. handler.Create.
//   - FormAction : info métadata PayZen (`REGISTER_PAY`,
//     `ASK_REGISTER_PAY`, `PAYMENT`…) conservée mais sans effet sur
//     l'enrôlement côté Paysim.
//   - NotificationURL : URL de destination du webhook émis par le
//     simulate ultérieur — indispensable en cas de rejeu direct
//     (charge_token), utile aussi côté one-shot pour tester le flux
//     de notification.
type CreatePayment struct {
	Provider        string            `yaml:"provider"`
	Amount          int64             `yaml:"amount"`
	Currency        string            `yaml:"currency"`
	OrderID         string            `yaml:"order_id"`
	FormAction      string            `yaml:"form_action,omitempty"`
	Customer        *Customer         `yaml:"customer,omitempty"`
	Metadata        map[string]string `yaml:"metadata,omitempty"`
	NotificationURL string            `yaml:"notification_url,omitempty"`
	Card            *Card             `yaml:"card,omitempty"`
}

// Customer decrit un client marchand associe au paiement. Seul l'email
// est supporte en scenario YAML pour l'instant — Paysim propage le
// bloc customer complet dans les webhooks (kr-answer.customer), utile
// pour tester que le marchand recoit bien les infos qu'il a envoyees.
type Customer struct {
	Email string `yaml:"email,omitempty"`
}

// Card décrit un moyen de paiement fictif présenté à Paysim.
// PAN complet, mois et année d'expiration, marque optionnelle
// (déduite du BIN si absente). Les tags JSON/YAML séparés permettent
// à la même struct d'être sérialisée côté client HTTP (camelCase
// PayZen) et parsée côté loader YAML (snake_case scénario).
//
// AVERTISSEMENT : les PAN sont stockés en clair côté serveur — ne
// jamais utiliser une CB réelle. Voir docs/testing-cards.md.
type Card struct {
	PAN         string `json:"pan"                yaml:"pan"`
	ExpiryMonth int    `json:"expiryMonth"        yaml:"expiry_month"`
	ExpiryYear  int    `json:"expiryYear"         yaml:"expiry_year"`
	Brand       string `json:"brand,omitempty"    yaml:"brand,omitempty"`
}

// ChargeToken déclenche un rejeu one-click d'un paiement à partir d'un
// paymentMethodToken déjà enregistré (via un create_payment précédent
// avec Card). Sans Token explicite, le runner utilise le dernier token
// vu — cohérent avec la mémorisation implicite de l'uuid par
// assert_state. Amount peut différer du montant initial, comme dans un
// vrai abonnement où chaque échéance a son propre montant.
type ChargeToken struct {
	Token           string `yaml:"token,omitempty"`
	Provider        string `yaml:"provider,omitempty"`
	Amount          int64  `yaml:"amount"`
	Currency        string `yaml:"currency"`
	OrderID         string `yaml:"order_id"`
	NotificationURL string `yaml:"notification_url,omitempty"`
}

// CreateSubscription crée un abonnement PSP-driven — Paysim retient la
// définition (moyen de paiement, montant, rrule, effect_date), l'appelant
// déclenche ensuite chaque échéance via trigger_billing (pas de moteur
// RRule qui tourne en fond côté simulateur, choix explicite 4.4.6).
// Token vide → utilise le dernier paymentMethodToken vu, cohérent avec
// charge_token. Provider vide → payzen par défaut.
type CreateSubscription struct {
	Provider   string            `yaml:"provider,omitempty"`
	Token      string            `yaml:"token,omitempty"`
	Amount     int64             `yaml:"amount"`
	Currency   string            `yaml:"currency"`
	OrderID    string            `yaml:"order_id"`
	EffectDate string            `yaml:"effect_date,omitempty"`
	Rrule      string            `yaml:"rrule,omitempty"`
	Metadata   map[string]string `yaml:"metadata,omitempty"`
}

// TriggerBilling déclenche manuellement une échéance d'un abonnement.
// Le paiement résultant devient le paiement courant (currentUUID) pour
// les assertions suivantes. SubscriptionID vide → utilise
// state.currentSubID (dernier abonnement créé).
type TriggerBilling struct {
	SubscriptionID string `yaml:"subscription_id,omitempty"`
}

// AssertSubscription vérifie l'existence d'un abonnement et
// optionnellement son état cancelled. Cancelled est un pointeur pour
// distinguer « non fourni, on ne vérifie que l'existence » de
// « fourni avec false, on veut cancelled=false ».
// SubscriptionID vide → utilise state.currentSubID.
type AssertSubscription struct {
	SubscriptionID string `yaml:"subscription_id,omitempty"`
	Cancelled      *bool  `yaml:"cancelled,omitempty"`
}

// CancelSubscription annule un abonnement. Idempotent côté serveur.
// SubscriptionID vide → utilise state.currentSubID.
type CancelSubscription struct {
	SubscriptionID string `yaml:"subscription_id,omitempty"`
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
// L'assertion attend que le compte soit atteint plutôt que de lire une
// seule fois : la livraison est asynchrone, le worker historise après
// que le handler a répondu. Timeout borne cette attente (5 s par
// défaut) — à relever quand un `inject` a retardé la livraison
// au-delà.
type AssertWebhook struct {
	Count   int      `yaml:"count"`
	Status  string   `yaml:"status,omitempty"`
	Timeout Duration `yaml:"timeout,omitempty"`
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
	case ActionChargeToken:
		s.ChargeToken = &ChargeToken{}
		return node.Decode(s.ChargeToken)
	case ActionCreateSubscription:
		s.CreateSubscription = &CreateSubscription{}
		return node.Decode(s.CreateSubscription)
	case ActionTriggerBilling:
		s.TriggerBilling = &TriggerBilling{}
		return node.Decode(s.TriggerBilling)
	case ActionAssertSubscription:
		s.AssertSubscription = &AssertSubscription{}
		return node.Decode(s.AssertSubscription)
	case ActionCancelSubscription:
		s.CancelSubscription = &CancelSubscription{}
		return node.Decode(s.CancelSubscription)
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
