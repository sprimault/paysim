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
	ErrCodeInvalidRequest      = "PAYSIM_INVALID_REQUEST"
	ErrCodeInvalidAmount       = "PAYSIM_INVALID_AMOUNT"
	ErrCodeInvalidCurrency     = "PAYSIM_INVALID_CURRENCY"
	ErrCodeInvalidPayment      = "PAYSIM_INVALID_PAYMENT"
	ErrCodeUUIDUnknown = "PAYSIM_UUID_UNKNOWN"
	// #nosec G101 -- code d'erreur, pas un secret.
	ErrCodeTokenUnknown        = "PAYSIM_TOKEN_UNKNOWN"
	ErrCodeSubscriptionUnknown = "PAYSIM_SUBSCRIPTION_UNKNOWN"
)

// CreatePaymentRequest est le corps JSON attendu par POST
// /api-payment/V4/Charge/CreatePayment. Les noms de champs sont ceux
// utilises par PayZen — on les recopie tels quels (regle providers.md).
type CreatePaymentRequest struct {
	OrderID    string            `json:"orderId"`
	Amount     format.Amount     `json:"amount"`   // centimes
	Currency   string            `json:"currency"` // ISO 4217
	FormAction string            `json:"formAction,omitempty"`
	Customer   Customer          `json:"customer,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
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
	UUID              string           `json:"uuid"`
	OrderID           string           `json:"orderId"`
	Amount            format.Amount    `json:"amount"`
	Currency          string           `json:"currency"`
	OrderStatus       domain.State     `json:"orderStatus"`
	PaymentMethodType string           `json:"paymentMethodType,omitempty"`
	CreationDate      string           `json:"creationDate"` // ISO 8601 UTC
	LastUpdateDate    string           `json:"lastUpdateDate"`
	Customer          Customer         `json:"customer,omitempty"`
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
type Transaction struct {
	FormToken  string
	UUID       string
	OrderID    string
	Amount     format.Amount
	Currency   string
	FormAction string
	Customer   Customer
	Metadata   map[string]string
	Payment    *domain.Payment
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
