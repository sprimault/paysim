// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package scenarios porte le format YAML des scénarios de test et son loader.
// Un scénario est une suite d'étapes typées que le runner applique
// contre un Paysim distant : création de paiement, avancement dans la
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
	ActionAssertPaymentMethod = "assert_payment_method"
	ActionAssertCustomer      = "assert_customer"
)

// Scenario est un scénario complet chargé depuis un fichier YAML. Une fois
// obtenu via Load ou LoadFile, tous ses champs sont conformes au format
// (name non vide, au moins une étape, chaque étape validée).
type Scenario struct {
	// Name identifie le scénario dans les rapports d'exécution.
	Name string `yaml:"name"`

	// Description explique ce que le scénario vérifie. Facultative
	// pour le format, précieuse pour qui relit un fichier six mois
	// plus tard.
	Description string `yaml:"description,omitempty"`

	// Steps est la suite ordonnée d'étapes. Au moins une : un scénario
	// vide est refusé au chargement.
	Steps []Step `yaml:"steps"`
}

// Step est une étape typée du scénario.
type Step struct {
	// Action est le discriminant lu depuis la clé `action:` du YAML.
	// Il détermine lequel des pointeurs ci-dessous est renseigné.
	Action string

	// Les pointeurs typés portent les champs propres à chaque action.
	// Exactement un est non nil après un Load réussi — le loader en
	// fait son invariant, les consommateurs peuvent s'y fier sans
	// revérifier.
	//
	// Trois familles : le paiement one-shot (création, simulation,
	// assertions), la récurrence pilotée par le marchand
	// (charge_token), et les abonnements pilotés par le PSP
	// (souscription, échéance, annulation).

	// CreatePayment crée un paiement, avec enrôlement de carte si une
	// `card` est fournie.
	CreatePayment *CreatePayment

	// Simulate joue l'acte de paiement à la place du porteur.
	Simulate *Simulate

	// Inject arme un mode de chaos consommé par le prochain simulate,
	// puis remis à zéro.
	Inject *Inject

	// Wait suspend l'exécution, pour laisser une livraison différée
	// arriver avant une assertion.
	Wait *Wait

	// AssertWebhook compte les livraisons, AssertState vérifie l'état
	// du paiement courant. Toutes deux échouent avec ErrAssertion, que
	// la CLI distingue d'une erreur d'exécution pour choisir son code
	// de retour.
	AssertWebhook *AssertWebhook
	AssertState   *AssertState

	// ChargeToken rejoue un paiement sur un moyen déjà enrôlé, sans
	// formulaire.
	ChargeToken *ChargeToken

	// CreateSubscription déclare un abonnement, TriggerBilling en
	// déclenche une échéance, AssertSubscription vérifie son état et
	// CancelSubscription l'annule.
	CreateSubscription *CreateSubscription
	TriggerBilling     *TriggerBilling
	AssertSubscription *AssertSubscription
	CancelSubscription *CancelSubscription

	// AssertPaymentMethod vérifie ce qui a réellement été enregistré à
	// l'enrôlement — marque, porteur, émetteur, exploitabilité.
	AssertPaymentMethod *AssertPaymentMethod

	// AssertCustomer vérifie le contexte marchand restitué par le
	// paiement courant.
	AssertCustomer *AssertCustomer
}

// CreatePayment demande à Paysim de créer un paiement via un provider.
//
// Fournir une Card présente un moyen de paiement. L'alias n'en sort
// qu'une fois l'issue connue : immédiatement quand le montant est nul —
// c'est une simple vérification —, au simulate sinon. Un refus n'en
// laisse aucun.
type CreatePayment struct {
	// Provider choisit l'adaptateur, "payzen" à défaut.
	Provider string `yaml:"provider"`

	// Amount en centimes entiers — jamais de flottant, invariant du
	// projet. Zéro est valide avec form_action REGISTER.
	Amount int64 `yaml:"amount"`

	// Currency en ISO 4217, OrderID libre.
	Currency string `yaml:"currency"`
	OrderID  string `yaml:"order_id"`

	// FormAction déclare l'intention PayZen, sans effet sur
	// l'enrôlement.
	FormAction string `yaml:"form_action,omitempty"`

	// Customer et Metadata permettent de vérifier que le contexte
	// marchand revient intact dans le webhook.
	Customer *Customer         `yaml:"customer,omitempty"`
	Metadata map[string]string `yaml:"metadata,omitempty"`

	// NotificationURL cible l'IPN émis par le simulate suivant.
	// Indispensable sur un rejeu direct, utile en one-shot pour
	// observer le flux de notification.
	NotificationURL string `yaml:"notification_url,omitempty"`

	// Card enrôle un moyen de paiement, dont le token est mémorisé par
	// le runner pour les charge_token suivants.
	Card *Card `yaml:"card,omitempty"`
}

// Customer decrit un client marchand associe au paiement. Paysim le
// restitue tel quel dans le kr-answer, ce qui permet a un scenario de
// verifier que le marchand retrouve ce qu'il a envoye.
type Customer struct {
	// Email de l'acheteur, restitué tel quel dans le webhook — de quoi
	// vérifier qu'un scénario retrouve ce qu'il a envoyé.
	Email string `json:"email,omitempty" yaml:"email,omitempty"`

	// Reference est l'identifiant du client côté marchand, à la racine
	// du bloc customer comme email. Permet de rapprocher un paiement
	// d'un compte sans passer par la metadata.
	Reference string `json:"reference,omitempty" yaml:"reference,omitempty"`

	// Les trois blocs du contexte client PayZen. Double jeu de tags
	// comme Card : la même struct est lue depuis le YAML en snake_case
	// et sérialisée vers l'API en camelCase.
	//
	// C'est ce qui évite la recopie champ par champ vers customerReq —
	// recopie où un champ oublié disparaissait sans erreur, exactement
	// le défaut que ces blocs servent à traquer.
	BillingDetails  *BillingDetails  `json:"billingDetails,omitempty"  yaml:"billing_details,omitempty"`
	ShippingDetails *ShippingDetails `json:"shippingDetails,omitempty" yaml:"shipping_details,omitempty"`
	ExtraDetails    *ExtraDetails    `json:"extraDetails,omitempty"    yaml:"extra_details,omitempty"`
}

// BillingDetails est l'adresse de facturation, telle qu'un scénario la
// déclare. Miroir de payzen.BillingDetails.
type BillingDetails struct {
	Language string `json:"language,omitempty"  yaml:"language,omitempty"`
	Title    string `json:"title,omitempty"     yaml:"title,omitempty"`

	FirstName string `json:"firstName,omitempty" yaml:"first_name,omitempty"`
	LastName  string `json:"lastName,omitempty"  yaml:"last_name,omitempty"`

	Address string `json:"address,omitempty"   yaml:"address,omitempty"`
	City    string `json:"city,omitempty"      yaml:"city,omitempty"`
	ZipCode string `json:"zipCode,omitempty"   yaml:"zip_code,omitempty"`
	Country string `json:"country,omitempty"   yaml:"country,omitempty"`
}

// ShippingDetails est l'adresse de livraison. Miroir de
// payzen.ShippingDetails — les énumérations y restent des chaînes
// libres, un scénario doit pouvoir écrire une valeur exotique sans que
// le loader la refuse.
type ShippingDetails struct {
	Category     string `json:"category,omitempty"     yaml:"category,omitempty"`
	LegalName    string `json:"legalName,omitempty"    yaml:"legal_name,omitempty"`
	IdentityCode string `json:"identityCode,omitempty" yaml:"identity_code,omitempty"`

	FirstName   string `json:"firstName,omitempty"   yaml:"first_name,omitempty"`
	LastName    string `json:"lastName,omitempty"    yaml:"last_name,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty" yaml:"phone_number,omitempty"`

	StreetNumber string `json:"streetNumber,omitempty" yaml:"street_number,omitempty"`
	Address      string `json:"address,omitempty"      yaml:"address,omitempty"`
	Address2     string `json:"address2,omitempty"     yaml:"address2,omitempty"`
	District     string `json:"district,omitempty"     yaml:"district,omitempty"`
	ZipCode      string `json:"zipCode,omitempty"      yaml:"zip_code,omitempty"`
	City         string `json:"city,omitempty"         yaml:"city,omitempty"`
	State        string `json:"state,omitempty"        yaml:"state,omitempty"`
	Country      string `json:"country,omitempty"      yaml:"country,omitempty"`

	DeliveryCompanyName string `json:"deliveryCompanyName,omitempty" yaml:"delivery_company_name,omitempty"`
	ShippingSpeed       string `json:"shippingSpeed,omitempty"       yaml:"shipping_speed,omitempty"`
	ShippingMethod      string `json:"shippingMethod,omitempty"      yaml:"shipping_method,omitempty"`
}

// ExtraDetails est le contexte navigateur. Miroir de
// payzen.ExtraDetails.
type ExtraDetails struct {
	IPAddress     string `json:"ipAddress,omitempty"     yaml:"ip_address,omitempty"`
	FingerPrintID string `json:"fingerPrintId,omitempty" yaml:"finger_print_id,omitempty"`

	BrowserUserAgent string `json:"browserUserAgent,omitempty" yaml:"browser_user_agent,omitempty"`
	BrowserAccept    string `json:"browserAccept,omitempty"    yaml:"browser_accept,omitempty"`
}

// Card décrit un moyen de paiement fictif présenté à Paysim. Le double
// jeu de tags permet à la même struct d'être sérialisée vers l'API
// (camelCase PayZen) et lue depuis un scénario (snake_case YAML).
//
// Les champs facultatifs alimentent le bloc cardDetails du kr-answer :
// ce sont eux qui rendent scriptables la carte étrangère, la carte de
// débit et le routage par émetteur.
//
// AVERTISSEMENT : les PAN sont stockés en clair côté serveur — ne
// jamais utiliser une CB réelle. Voir docs/testing-cards.md.
type Card struct {
	// PAN est le numéro complet, en clair. Aucune validation Luhn :
	// le simulateur est aveugle au contenu, sauf pour les quatre PAN
	// de test réservés qui déclenchent un refus.
	PAN string `json:"pan"                yaml:"pan"`

	// ExpiryMonth (1-12) et ExpiryYear (4 chiffres). Une date passée
	// fait refuser tout débit — c'est l'un des leviers de test.
	ExpiryMonth int `json:"expiryMonth"        yaml:"expiry_month"`
	ExpiryYear  int `json:"expiryYear"         yaml:"expiry_year"`

	// Brand est la marque, déduite du BIN si absente.
	Brand string `json:"brand,omitempty"    yaml:"brand,omitempty"`

	// HolderName est le nom du porteur, restitué tel quel.
	HolderName string `json:"holderName,omitempty"      yaml:"holder_name,omitempty"`

	// Country (ISO 3166-1 alpha-2), ProductCategory (CREDIT, DEBIT,
	// PREPAID) et IssuerName caractérisent la carte côté émetteur.
	// Défauts appliqués au rendu quand ils sont absents : FR, CREDIT,
	// PAYSIM.
	Country         string `json:"country,omitempty"         yaml:"country,omitempty"`
	ProductCategory string `json:"productCategory,omitempty" yaml:"product_category,omitempty"`
	IssuerName      string `json:"issuerName,omitempty"      yaml:"issuer_name,omitempty"`
}

// ChargeToken déclenche un rejeu one-click d'un paiement à partir d'un
// paymentMethodToken déjà enregistré (via un create_payment précédent
// avec Card). Sans Token explicite, le runner utilise le dernier token
// vu — cohérent avec la mémorisation implicite de l'uuid par
// assert_state. Amount peut différer du montant initial, comme dans un
// vrai abonnement où chaque échéance a son propre montant.
type ChargeToken struct {
	// Token désigne le moyen à débiter. Vide, le runner reprend le
	// dernier enrôlé — ce qui rend le scénario courant lisible sans
	// recopier un identifiant.
	Token string `yaml:"token,omitempty"`

	// Provider choisit l'adaptateur, payzen à défaut.
	Provider string `yaml:"provider,omitempty"`

	// Amount peut différer du paiement initial : chaque échéance d'un
	// abonnement a son propre montant. En centimes entiers.
	Amount int64 `yaml:"amount"`

	// Currency en ISO 4217, OrderID libre.
	Currency string `yaml:"currency"`
	OrderID  string `yaml:"order_id"`

	// NotificationURL cible l'IPN. Absente, le serveur retombe sur sa
	// configuration globale — un rejeu notifie toujours.
	NotificationURL string `yaml:"notification_url,omitempty"`

	// Customer permet d'envoyer un contexte client sur le rejeu. Chez
	// un vrai PSP, reference, email et billingDetails y sont ignorés au
	// profit de ceux de l'alias : le renseigner sert justement à
	// vérifier que Paysim fait de même.
	Customer *Customer `yaml:"customer,omitempty"`
}

// CreateSubscription crée un abonnement PSP-driven — Paysim retient la
// définition (moyen de paiement, montant, rrule, effect_date), l'appelant
// déclenche ensuite chaque échéance via trigger_billing (pas de moteur
// RRule qui tourne en fond côté simulateur, choix explicite).
// Token vide → utilise le dernier paymentMethodToken vu, cohérent avec
// charge_token. Provider vide → payzen par défaut.
type CreateSubscription struct {
	// Provider choisit l'adaptateur, payzen à défaut.
	Provider string `yaml:"provider,omitempty"`

	// Token désigne le moyen à prélever. Vide, le runner reprend le
	// dernier paymentMethodToken vu — un scénario enchaîne
	// généralement enrôlement puis souscription.
	Token string `yaml:"token,omitempty"`

	// Amount en centimes entiers, Currency en ISO 4217, OrderID libre.
	Amount   int64  `yaml:"amount"`
	Currency string `yaml:"currency"`
	OrderID  string `yaml:"order_id"`

	// EffectDate et Rrule déclarent l'échéancier. Restitués tels quels
	// mais jamais interprétés : chaque échéance se déclenche par un
	// trigger_billing explicite.
	EffectDate string `yaml:"effect_date,omitempty"`
	Rrule      string `yaml:"rrule,omitempty"`

	// Metadata est recopiée sur chaque échéance.
	Metadata map[string]string `yaml:"metadata,omitempty"`
}

// TriggerBilling déclenche manuellement une échéance d'un abonnement.
// Le paiement résultant devient le paiement courant (currentUUID) pour
// les assertions suivantes. SubscriptionID vide → utilise
// state.currentSubID (dernier abonnement créé).
type TriggerBilling struct {
	// SubscriptionID désigne l'abonnement à facturer. Vide, le runner
	// reprend le dernier créé.
	SubscriptionID string `yaml:"subscription_id,omitempty"`
}

// AssertSubscription vérifie l'existence d'un abonnement et
// optionnellement son état cancelled. Cancelled est un pointeur pour
// distinguer « non fourni, on ne vérifie que l'existence » de
// « fourni avec false, on veut cancelled=false ».
// SubscriptionID vide → utilise state.currentSubID.
type AssertSubscription struct {
	// SubscriptionID désigne l'abonnement à vérifier. Vide, le runner
	// reprend le dernier créé.
	SubscriptionID string `yaml:"subscription_id,omitempty"`

	// Cancelled est un pointeur pour distinguer trois cas là où un
	// booléen n'en donnerait que deux : absent, on vérifie seulement
	// l'existence ; false, on exige un abonnement actif ; true, un
	// abonnement annulé.
	Cancelled *bool `yaml:"cancelled,omitempty"`
}

// AssertPaymentMethod vérifie les attributs du moyen de paiement
// enregistré. Tous les champs sont optionnels : seuls ceux renseignés
// sont comparés, les autres sont ignorés.
//
// Cette assertion existe parce que les scénarios pouvaient jusqu'ici
// enrôler une carte avec un porteur, un pays et un émetteur donnés sans
// jamais vérifier ce qui en était retenu. Ils prouvaient que
// l'enrôlement n'échouait pas, pas qu'il enregistrait les bonnes
// valeurs — c'est ce trou qui a laissé passer plusieurs défauts trouvés
// en intégration réelle plutôt qu'en test.
//
// Token vide → utilise le dernier moyen enrôlé.
type AssertPaymentMethod struct {
	// Token désigne le moyen à vérifier. Vide, le runner reprend le
	// dernier enrôlé.
	Token string `yaml:"token,omitempty"`

	// Brand est la marque attendue (VISA, MASTERCARD, CB, AMEX…).
	Brand string `yaml:"brand,omitempty"`

	// PANMasked est le numéro tronqué attendu, au format du provider
	// ("555555XXXXXX4444" pour PayZen).
	PANMasked string `yaml:"pan_masked,omitempty"`

	// HolderName, Country, ProductCategory et IssuerName sont les
	// attributs du porteur et de l'émetteur, tels qu'ils ont été
	// transmis à l'enrôlement.
	HolderName      string `yaml:"holder_name,omitempty"`
	Country         string `yaml:"country,omitempty"`
	ProductCategory string `yaml:"product_category,omitempty"`
	IssuerName      string `yaml:"issuer_name,omitempty"`

	// Usable est un pointeur pour distinguer « non fourni, on ne
	// vérifie pas » de « fourni avec false, on exige un moyen
	// inexploitable » — même raison que Cancelled sur les abonnements.
	Usable *bool `yaml:"usable,omitempty"`

	// UnusableReason est le motif attendu quand le moyen ne l'est pas
	// ("moyen de paiement revoque", "carte de test refusee"…).
	UnusableReason string `yaml:"unusable_reason,omitempty"`
}

// AssertCustomer vérifie le contexte marchand tel que le paiement
// courant le restitue. Réutilise la struct Customer des scénarios : les
// champs renseignés sont comparés, les autres ignorés.
//
// Deux usages, tous deux nés de défauts trouvés en intégration :
// contrôler qu'un champ envoyé revient intact — reference, livraison,
// contexte navigateur ont chacun disparu en silence à un moment — et
// contrôler qu'un rejeu par alias restitue le client de l'alias, pas
// celui de la requête.
type AssertCustomer struct {
	// UUID désigne le paiement à vérifier. Vide, le runner reprend le
	// dernier créé.
	UUID string `yaml:"uuid,omitempty"`

	// Expect porte les valeurs attendues, dans la même forme que le
	// bloc customer d'un create_payment.
	Expect Customer `yaml:"expect"`
}

// CancelSubscription annule un abonnement. Idempotent côté serveur.
// SubscriptionID vide → utilise state.currentSubID.
type CancelSubscription struct {
	// SubscriptionID désigne l'abonnement à annuler. Vide, le runner
	// reprend le dernier créé.
	SubscriptionID string `yaml:"subscription_id,omitempty"`
}

// Simulate avance le paiement dans la machine à états via l'API de simulation
// de Paysim. Status est l'état cible tel que perçu côté API (`captured`,
// `refunded`, `declined`…), pas un statut protocolaire de fournisseur.
type Simulate struct {
	// Status est l'état visé, en vocabulaire du domaine — captured,
	// declined, authorized… Le runner le traduit vers l'outcome du
	// provider, de sorte qu'un scénario reste lisible quand un second
	// fournisseur arrive.
	Status string `yaml:"status"`
}

// Inject active un mode de panne du moteur de chaos pour les webhooks émis à
// partir de l'étape suivante. Le vocabulaire de Mode suit celui du paquet
// internal/chaos (`duplicate`, `delay`, `bad-signature`, `race`).
type Inject struct {
	// Mode nomme la panne à armer : duplicate, bad-signature, race, ou
	// delay=NNN en millisecondes. Un mode inconnu échoue franchement
	// plutôt que d'être ignoré — un chaos qui ne se déclenche pas sans
	// le dire vaut moins que pas de chaos du tout.
	Mode string `yaml:"mode"`
}

// Wait suspend l'exécution du scénario pendant Duration. Utile pour laisser
// la file de livraison drainer un webhook différé avant une assertion.
type Wait struct {
	// Duration au format Go — 500ms, 2s, 1m30s.
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
// Deux filtres, deux questions distinctes. Status porte sur
// l'acheminement HTTP (`delivered`, `failed`, `pending`) : le webhook
// est-il arrivé ? Outcome porte sur le résultat métier annoncé dans le
// corps (`PAID`, `UNPAID`… en vocabulaire provider) : qu'annonçait-il ?
// Un webhook remis avec succès peut parfaitement annoncer un refus —
// les confondre, c'est asserter autre chose que ce qu'on croit.
// Cumulables : les deux doivent être satisfaits.
type AssertWebhook struct {
	// Count est le nombre attendu, comparé strictement. Un écart
	// signale soit un chaos non prévu, soit un défaut de la simulation.
	Count int `yaml:"count"`

	// Status filtre sur l'acheminement (delivered, failed, pending),
	// Outcome sur le résultat métier (PAID, UNPAID…). Cumulables : les
	// deux doivent alors être satisfaits.
	Status  string `yaml:"status,omitempty"`
	Outcome string `yaml:"outcome,omitempty"`

	// Timeout borne l'attente. À relever quand un inject a retardé la
	// livraison au-delà du défaut.
	Timeout Duration `yaml:"timeout,omitempty"`
}

// AssertState vérifie que le paiement courant est dans l'état State (nom
// canonique de la machine à états : `initiated`, `authorized`, `captured`,
// `partially_refunded`, `refunded`, `declined`, `expired`, `chargeback`).
type AssertState struct {
	// State attendu, en vocabulaire du domaine. Voir docs/states.md
	// pour la liste et les transitions permises.
	State string `yaml:"state"`

	// DeclineCode attendu : le code de retour d'autorisation ISO 8583
	// du refus — 51 pour une provision insuffisante, 43 pour une
	// opposition, 91 pour un émetteur injoignable.
	//
	// Optionnel, et vérifié seulement s'il est renseigné : la plupart
	// des scénarios se moquent du motif, mais ceux qui l'exercent
	// doivent pouvoir le figer. C'est ce couple qui décide de la
	// reconduction chez le marchand, or rien ne le couvrait de bout en
	// bout — un motif perdu en route ne se serait vu nulle part.
	DeclineCode string `yaml:"decline_code,omitempty"`
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
	case ActionAssertPaymentMethod:
		s.AssertPaymentMethod = &AssertPaymentMethod{}
		return node.Decode(s.AssertPaymentMethod)
	case ActionAssertCustomer:
		s.AssertCustomer = &AssertCustomer{}
		return node.Decode(s.AssertCustomer)
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
