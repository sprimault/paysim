// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"time"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
)

// APIResponse est l'enveloppe standard des reponses V4 : status + answer
// opaque. PayZen renvoie beaucoup plus de champs (webService, version,
// serverDate, mode, applicationVersion, ticket...), on les ajoute au fur
// et a mesure si un client s'en plaint — pas de sur-modelisation a froid.
type APIResponse struct {
	Status string          `json:"status"`
	Answer json.RawMessage `json:"answer"`
}

// APIError est la structure retournee dans answer quand status vaut
// "ERROR". Format aligne sur celui de PayZen pour rester interoperable
// avec les SDK marchand qui inspectent ces champs.
type APIError struct {
	ErrorCode            string `json:"errorCode"`
	ErrorMessage         string `json:"errorMessage"`
	DetailedErrorCode    string `json:"detailedErrorCode,omitempty"`
	DetailedErrorMessage string `json:"detailedErrorMessage,omitempty"`
}

// Codes d'erreur Paysim. Prefixe PAYSIM_ pour ne pas se confondre avec
// les codes reels de PayZen (INT_010, PSP_010, ACQ_010...). Le jour ou
// un client attend un code PayZen precis, on mappera ici.
const (
	ErrCodeInvalidRequest  = "PAYSIM_INVALID_REQUEST"
	ErrCodeInvalidAmount   = "PAYSIM_INVALID_AMOUNT"
	ErrCodeInvalidCurrency = "PAYSIM_INVALID_CURRENCY"
	ErrCodeInvalidPayment  = "PAYSIM_INVALID_PAYMENT"
	ErrCodeUUIDUnknown     = "PAYSIM_UUID_UNKNOWN"
	// #nosec G101 -- code d'erreur, pas un secret.
	ErrCodeTokenUnknown        = "PAYSIM_TOKEN_UNKNOWN"
	ErrCodeSubscriptionUnknown = "PAYSIM_SUBSCRIPTION_UNKNOWN"
	ErrCodeStoreFailure        = "PAYSIM_STORE_FAILURE"
)

// CreatePaymentRequest est le corps JSON attendu par POST
// /api-payment/V4/Charge/CreatePayment. Les noms de champs sont ceux
// utilises par PayZen — on les recopie tels quels (regle providers.md).
//
// ReturnURL et NotificationURL sont des extensions propres a Paysim
// (non-standard PayZen) : le vrai back-office porte cette configuration
// statiquement, mais un simulateur profite d'un contrat par requete.
// Optionnelles — si absentes, l'appel de simulation devra les fournir.
type CreatePaymentRequest struct {
	OrderID         string            `json:"orderId"`
	Amount          format.Amount     `json:"amount"`   // centimes
	Currency        string            `json:"currency"` // ISO 4217
	FormAction      string            `json:"formAction,omitempty"`
	Customer        Customer          `json:"customer,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	ReturnURL       string            `json:"returnUrl,omitempty"`
	NotificationURL string            `json:"notificationUrl,omitempty"`
}

// CreatePaymentAnswer est le contenu de answer sur succes.
type CreatePaymentAnswer struct {
	FormToken string `json:"formToken"`
}

// TransactionGetRequest est le corps de POST /api-payment/V4/Transaction/Get.
type TransactionGetRequest struct {
	UUID string `json:"uuid"`
}

// TransactionGetAnswer est le resume d'une transaction retourne au marchand.
// Miroir simplifie de la structure des transactions[0] dans kr-answer :
// on garde ce qui est utile pour le controle cote marchand, on n'invente
// pas ce qu'on ne peut pas remplir.
type TransactionGetAnswer struct {
	UUID              string            `json:"uuid"`
	OrderID           string            `json:"orderId"`
	Amount            format.Amount     `json:"amount"`
	Currency          string            `json:"currency"`
	OrderStatus       domain.State      `json:"orderStatus"`
	PaymentMethodType string            `json:"paymentMethodType,omitempty"`
	CreationDate      string            `json:"creationDate"` // ISO 8601 UTC
	LastUpdateDate    string            `json:"lastUpdateDate"`
	Customer          Customer          `json:"customer,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// Customer et BillingDetails miroir de la structure PayZen — aucun
// champ obligatoire cote domain, on stocke pour le rendre dans les
// retours. Les noms conservent la casse PayZen.
type Customer struct {
	Email          string         `json:"email,omitempty"`
	BillingDetails BillingDetails `json:"billingDetails,omitempty"`
}

// BillingDetails represente l'adresse de facturation. Aucun champ
// n'est obligatoire — le simulateur les propage tels quels.
type BillingDetails struct {
	Language  string `json:"language,omitempty"`
	Title     string `json:"title,omitempty"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Address   string `json:"address,omitempty"`
	City      string `json:"city,omitempty"`
	ZipCode   string `json:"zipCode,omitempty"`
	Country   string `json:"country,omitempty"`
}

// Transaction est le contexte complet d'un paiement PayZen simule.
// Un formToken opaque cote marchand pointe sur cette structure. Elle
// porte aussi l'aggregat domain.Payment, qui reste la source de verite
// pour la machine a etats — les autres champs sont metadonnees pour
// remplir les payloads V4.
//
// ReturnURL et NotificationURL sont stockees ici a titre de valeurs
// par defaut pour les appels de simulation. L'appel de simulation
// peut les surcharger.
type Transaction struct {
	FormToken       string
	UUID            string
	OrderID         string
	Amount          format.Amount
	Currency        string
	FormAction      string
	Customer        Customer
	Metadata        map[string]string
	Payment         *domain.Payment
	ReturnURL       string
	NotificationURL string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// UpdatePaymentRequest est le corps de POST /api-payment/V4/Charge/UpdatePayment.
// Met a jour le contexte d'un formulaire deja cree (typiquement les
// coordonnees du client apres modification cote UI). Ne change pas
// l'etat du domain.Payment associe.
type UpdatePaymentRequest struct {
	FormToken string            `json:"formToken"`
	Customer  Customer          `json:"customer,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// UpdatePaymentAnswer est le contenu de answer sur succes — le meme
// formToken (inchange), pour permettre au marchand de continuer sans
// nouvelle initialisation cote SmartForm.
type UpdatePaymentAnswer struct {
	FormToken string `json:"formToken"`
}

// CreateSubscriptionRequest est le corps de POST
// /api-payment/V4/Charge/CreateSubscription. Cree un abonnement
// recurrent lie a un paymentMethodToken deja obtenu (typiquement
// issu d'un formulaire REGISTER_PAY precedent).
//
// EffectDate et Rrule reprennent le vocabulaire iCalendar (RFC 5545) —
// c'est ce que PayZen expose, on recopie tel quel.
type CreateSubscriptionRequest struct {
	OrderID            string            `json:"orderId,omitempty"`
	Amount             format.Amount     `json:"amount"`
	Currency           string            `json:"currency"`
	PaymentMethodToken string            `json:"paymentMethodToken"`
	EffectDate         string            `json:"effectDate"` // ISO 8601
	Rrule              string            `json:"rrule"`      // ex "RRULE:FREQ=MONTHLY;INTERVAL=1"
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// CreateSubscriptionAnswer est le contenu de answer sur succes.
type CreateSubscriptionAnswer struct {
	SubscriptionID string `json:"subscriptionId"`
}

// SubscriptionGetRequest est le corps de POST /api-payment/V4/Subscription/Get.
// PayZen exige a la fois subscriptionId et paymentMethodToken —
// double index qui rend la requete moins ambigue.
type SubscriptionGetRequest struct {
	SubscriptionID     string `json:"subscriptionId"`
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`
}

// SubscriptionGetAnswer est le resume d'un abonnement retourne au marchand.
// Miroir simplifie de ce que PayZen renvoie — on ne modelise pas les
// occurrences de facturation (renewals) en phase 1.
type SubscriptionGetAnswer struct {
	SubscriptionID     string            `json:"subscriptionId"`
	OrderID            string            `json:"orderId,omitempty"`
	Amount             format.Amount     `json:"amount"`
	Currency           string            `json:"currency"`
	EffectDate         string            `json:"effectDate"`
	Rrule              string            `json:"rrule"`
	PaymentMethodToken string            `json:"paymentMethodToken"`
	CreationDate       string            `json:"creationDate"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// Subscription est le contexte d'un abonnement simule cote Paysim.
// Stub minimaliste en phase 1 : aucune mecanique de facturation
// periodique, aucun etat "annule" ou "suspendu". A etendre si le
// besoin d'une vraie simulation des renewals s'exprime.
type Subscription struct {
	ID                 string
	OrderID            string
	Amount             format.Amount
	Currency           string
	PaymentMethodToken string
	EffectDate         string
	Rrule              string
	Metadata           map[string]string
	CreatedAt          time.Time
}

// Outcomes supportes pour les APIs de controle
// /paysim/simulate/browserReturn et /paysim/simulate/ipn.
//
// ABANDONED est mappe sur domain.Expire() : semantiquement l'utilisateur
// est parti, le paiement n'a jamais reussi — cf. le status EXPIRED cote
// domaine (pas de nouvel etat "abandoned" au domain, ce serait de la
// dette pour une nuance metier faible).
const (
	OutcomePaid       = "PAID"
	OutcomeAuthorised = "AUTHORISED"
	OutcomeUnpaid     = "UNPAID"
	OutcomeExpired    = "EXPIRED"
	OutcomeAbandoned  = "ABANDONED"
)

// WebhookChaos regroupe les injections de pannes appliquees au webhook
// resultant de la simulation. Chaque flag est independant, tous
// inertes par defaut (invariant 5).
type WebhookChaos struct {
	// Duplicate : le meme webhook est enqueue deux fois — le marchand
	// doit gerer l'idempotence.
	Duplicate bool `json:"duplicate,omitempty"`

	// BadSignature : le kr-hash envoye est altere. Le marchand qui
	// verifie la signature doit refuser le webhook.
	BadSignature bool `json:"badSignature,omitempty"`

	// RaceBeforeResponse : la reponse HTTP au simulate est retardee
	// de 500 ms, laissant le temps au webhook de partir en avance.
	// Cote client, la webhook arrive avant la reponse a l'appel de
	// simulation — la course la plus dure a reproduire en prod.
	RaceBeforeResponse bool `json:"raceBeforeResponse,omitempty"`
}

// BrowserReturnRequest est le corps de POST /paysim/simulate/browserReturn.
// Endpoint de controle Paysim (pas PayZen) : le marchand demande a
// Paysim de simuler la fin d'un parcours et l'envoi du retour signe
// vers son URL. Les champs optionnels ont des valeurs par defaut
// coherentes avec un paiement CB VISA reussi 3DS SUCCESS.
//
// DeliveryDelayMs retarde l'envoi du webhook resultant. Compose avec
// deux appels successifs pour simuler du out-of-order sans flag dedie.
type BrowserReturnRequest struct {
	FormToken         string       `json:"formToken"`
	ReturnURL         string       `json:"returnUrl,omitempty"`         // surcharge la ReturnURL de la Transaction
	Outcome           string       `json:"outcome"`                     // PAID | AUTHORISED | UNPAID | EXPIRED
	PaymentMethodType string       `json:"paymentMethodType,omitempty"` // defaut CARDS
	CardBrand         string       `json:"cardBrand,omitempty"`         // defaut VISA
	Wallet            string       `json:"wallet,omitempty"`            // ex APPLE_PAY, GOOGLEPAY
	ThreeDSStatus     string       `json:"threeDSStatus,omitempty"`     // defaut SUCCESS
	ErrorCode         string       `json:"errorCode,omitempty"`         // pour UNPAID
	ErrorMessage      string       `json:"errorMessage,omitempty"`
	Chaos             WebhookChaos `json:"chaos,omitempty"`
	DeliveryDelayMs   int          `json:"deliveryDelayMs,omitempty"`
}

// BrowserReturnResponse est le corps de reponse a l'API de controle.
// Renvoie le hash calcule pour permettre au marchand de le comparer
// dans un test d'integration (diagnostic seulement, pas de secret).
type BrowserReturnResponse struct {
	Status     string `json:"status"`
	DeliveryID string `json:"deliveryId,omitempty"`
	KrHash     string `json:"krHash,omitempty"`
}

// IPNRequest est le corps de POST /paysim/simulate/ipn. Meme mecanique
// que BrowserReturnRequest, mais le POST resultant part vers
// NotificationURL (webhook serveur-a-serveur) au lieu de ReturnURL
// (retour navigateur). La distinction est logique : deux endpoints
// distincts pour deux flux distincts cote marchand, meme si le
// contenu du POST est identique.
type IPNRequest struct {
	FormToken         string       `json:"formToken"`
	NotificationURL   string       `json:"notificationUrl,omitempty"`
	Outcome           string       `json:"outcome"`
	PaymentMethodType string       `json:"paymentMethodType,omitempty"`
	CardBrand         string       `json:"cardBrand,omitempty"`
	Wallet            string       `json:"wallet,omitempty"`
	ThreeDSStatus     string       `json:"threeDSStatus,omitempty"`
	ErrorCode         string       `json:"errorCode,omitempty"`
	ErrorMessage      string       `json:"errorMessage,omitempty"`
	Chaos             WebhookChaos `json:"chaos,omitempty"`
	DeliveryDelayMs   int          `json:"deliveryDelayMs,omitempty"`
}

// IPNResponse est le corps de reponse — identique en structure a
// BrowserReturnResponse.
type IPNResponse struct {
	Status     string `json:"status"`
	DeliveryID string `json:"deliveryId,omitempty"`
	KrHash     string `json:"krHash,omitempty"`
}

// KrAnswer est la structure JSON sérialisée et envoyée dans le champ
// POST kr-answer du retour navigateur / du webhook IPN. Structure plate
// au top-level (pas de wrapper `{status, answer}` — celui-ci concerne
// uniquement les reponses de l'API REST V4).
//
// Les champs `_type` sont un artefact du sérialiseur Java de PayZen
// (discriminateur pour la deserialisation cote SDK) — on les reproduit
// litteralement (invariant 3).
type KrAnswer struct {
	ShopID             string          `json:"shopId,omitempty"`
	OrderCycle         string          `json:"orderCycle"`
	OrderStatus        string          `json:"orderStatus"`
	ServerDate         string          `json:"serverDate"`
	ServerURL          string          `json:"serverUrl,omitempty"`
	ApplicationVersion string          `json:"applicationVersion,omitempty"`
	Mode               string          `json:"mode"`
	OrderDetails       KrOrderDetails  `json:"orderDetails"`
	Customer           Customer        `json:"customer,omitempty"`
	Transactions       []KrTransaction `json:"transactions"`
	SubscriptionID     string          `json:"subscriptionId,omitempty"`
	Type               string          `json:"_type"`
}

// KrOrderDetails contient les infos de commande cote PayZen.
type KrOrderDetails struct {
	OrderTotalAmount     format.Amount `json:"orderTotalAmount"`
	OrderCurrency        string        `json:"orderCurrency"`
	Mode                 string        `json:"mode"`
	OrderID              string        `json:"orderId"`
	OrderEffectiveAmount format.Amount `json:"orderEffectiveAmount"`
	Type                 string        `json:"_type"`
}

// KrTransaction est un element du tableau transactions[]. En phase 1
// on n'a qu'une entree par retour (pas de paiements en plusieurs fois).
type KrTransaction struct {
	ShopID             string               `json:"shopId,omitempty"`
	UUID               string               `json:"uuid"`
	Amount             format.Amount        `json:"amount"`
	Currency           string               `json:"currency"`
	PaymentMethodType  string               `json:"paymentMethodType"`
	PaymentMethodToken string               `json:"paymentMethodToken,omitempty"`
	Status             string               `json:"status"`         // PAID | UNPAID
	DetailedStatus     string               `json:"detailedStatus"` // AUTHORISED | CAPTURED | REFUSED | ...
	OperationType      string               `json:"operationType"`  // DEBIT | CREDIT
	CreationDate       string               `json:"creationDate"`
	ErrorCode          string               `json:"errorCode,omitempty"`
	ErrorMessage       string               `json:"errorMessage,omitempty"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
	TransactionDetails KrTransactionDetails `json:"transactionDetails"`
	Type               string               `json:"_type"`
}

// KrTransactionDetails porte les details specifiques a la methode de
// paiement utilisee (CB, wallet, virement...). CardDetails et
// ThreeDSResponse sont des pointeurs pour permettre leur omission
// naturelle (omitempty) quand la methode ne les porte pas.
type KrTransactionDetails struct {
	Mid             string             `json:"mid,omitempty"`
	CreationContext string             `json:"creationContext"`
	Wallet          string             `json:"wallet,omitempty"`
	CardDetails     *KrCardDetails     `json:"cardDetails,omitempty"`
	ThreeDSResponse *KrThreeDSResponse `json:"threeDSResponse,omitempty"`
	Type            string             `json:"_type"`
}

// KrCardDetails contient les infos carte visibles cote marchand.
// PAN toujours masque — la vraie PAN n'entre jamais dans un retour PSP,
// invariant PCI-DSS. Le simulateur reproduit ce masquage tel quel.
type KrCardDetails struct {
	PAN             string `json:"pan"`
	Brand           string `json:"brand"`
	ProductCategory string `json:"productCategory,omitempty"`
	ExpiryMonth     int    `json:"expiryMonth"`
	ExpiryYear      int    `json:"expiryYear"`
	Country         string `json:"country,omitempty"`
	IssuerName      string `json:"issuerName,omitempty"`
	EffectiveBrand  string `json:"effectiveBrand,omitempty"`
	Type            string `json:"_type"`
}

// KrThreeDSResponse porte le resultat de l'authentification 3D Secure
// telle qu'elle a ete portee par le SmartForm cote client.
type KrThreeDSResponse struct {
	AuthenticationResultData KrAuthenticationResultData `json:"authenticationResultData"`
	Type                     string                     `json:"_type"`
}

// KrAuthenticationResultData porte le status precis du 3DS.
// Status : SUCCESS | FAILURE | NOT_ENROLLED | UNAVAILABLE.
// AuthenticationType : FRICTIONLESS | CHALLENGE.
type KrAuthenticationResultData struct {
	Status             string `json:"status"`
	AuthenticationType string `json:"authenticationType,omitempty"`
	Type               string `json:"_type"`
}
