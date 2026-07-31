// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/format"
)

// applicationVersion est la version du serveur PayZen simulé —
// arbitraire, cohérente avec ce qu'un vrai back-office annoncerait.
const applicationVersion = "6.0.0-paysim"

// mapping outcome → contexte de transition et status/detailedStatus
// PayZen. Regroupé ici pour rester une seule source de vérité et
// éviter les switch dispersés.
type outcomeSpec struct {
	OrderStatus    string
	OrderCycle     string
	TxStatus       string
	DetailedStatus string
	OperationType  string
}

var outcomeSpecs = map[string]outcomeSpec{
	OutcomePaid: {
		OrderStatus: "PAID", OrderCycle: "CLOSED",
		TxStatus: "PAID", DetailedStatus: "CAPTURED", OperationType: "DEBIT",
	},
	OutcomeAuthorised: {
		OrderStatus: "PAID", OrderCycle: "OPEN",
		TxStatus: "PAID", DetailedStatus: "AUTHORISED", OperationType: "DEBIT",
	},
	OutcomeUnpaid: {
		OrderStatus: "UNPAID", OrderCycle: "CLOSED",
		TxStatus: "UNPAID", DetailedStatus: "REFUSED", OperationType: "DEBIT",
	},
	OutcomeExpired: {
		OrderStatus: "EXPIRED", OrderCycle: "CLOSED",
		TxStatus: "UNPAID", DetailedStatus: "EXPIRED", OperationType: "DEBIT",
	},
	OutcomeAbandoned: {
		OrderStatus: "ABANDONED", OrderCycle: "CLOSED",
		TxStatus: "UNPAID", DetailedStatus: "ABANDONED", OperationType: "DEBIT",
	},
}

// ErrUnknownOutcome est retournée par applyOutcome sur une valeur
// inconnue. Sentinelle interne au paquet — le handler la convertit
// en réponse HTTP appropriée.
var ErrUnknownOutcome = errors.New("outcome inconnu")

// applyOutcome fait progresser le domain.Payment de la transaction
// selon l'outcome demandé. Miroir des transitions autorisées par le
// domaine — toute divergence produit une erreur domain (par exemple
// PAID sur un paiement déjà capturé). Le mapping ABANDONED→Expire est
// documenté dans les constantes.
func applyOutcome(tx *Transaction, outcome, reason string) error {
	switch outcome {
	case OutcomePaid:
		return tx.Payment.Capture()
	case OutcomeAuthorised:
		return tx.Payment.Authorize()
	case OutcomeUnpaid:
		if reason == "" {
			reason = "simulation"
		}
		return tx.Payment.Decline(reason)
	case OutcomeExpired, OutcomeAbandoned:
		return tx.Payment.Expire()
	}
	return ErrUnknownOutcome
}

// buildKrAnswer construit le KrAnswer à envoyer au marchand. Fonction
// pure : reçoit un Transaction (avec Payment déjà transité) et les
// options de la requête de simulation, retourne la structure JSON à
// signer. Ne modifie rien, n'écrit pas.
//
// Défauts appliqués : paymentMethodType=CARDS, cardBrand=VISA,
// threeDSStatus=SUCCESS, authenticationType déduit du status.
func buildKrAnswer(tx *Transaction, opts BrowserReturnOpts, serverURL string, mode string) *KrAnswer {
	spec := outcomeSpecs[opts.Outcome]
	now := time.Now().UTC().Format(time.RFC3339)

	paymentMethodType := opts.PaymentMethodType
	if paymentMethodType == "" {
		paymentMethodType = "CARDS"
	}
	cardBrand := opts.CardBrand
	if cardBrand == "" {
		cardBrand = "VISA"
	}
	threeDSStatus := opts.ThreeDSStatus
	if threeDSStatus == "" {
		threeDSStatus = "SUCCESS"
	}

	// CardDetails seulement quand la methode est CARDS — un virement
	// (IP_WIRE) ou un wallet portent d'autres blocs, qu'on ne modelise
	// pas encore ici (extension non-breaking pour plus tard).
	var cardDetails *KrCardDetails
	if paymentMethodType == "CARDS" || paymentMethodType == "CB" {
		cardDetails = &KrCardDetails{
			PAN:             newMaskedPAN(cardBrand),
			Brand:           cardBrand,
			ProductCategory: "CREDIT",
			ExpiryMonth:     12,
			ExpiryYear:      time.Now().UTC().Year() + 2,
			Country:         "FR",
			IssuerName:      "PAYSIM",
			EffectiveBrand:  cardBrand,
			Type:            "V4/CardDetails",
		}
	}

	authType := "FRICTIONLESS"
	if threeDSStatus == "CHALLENGE" {
		authType = "CHALLENGE"
	}
	threeDS := &KrThreeDSResponse{
		AuthenticationResultData: KrAuthenticationResultData{
			Status:             threeDSStatus,
			AuthenticationType: authType,
			Type:               "V4/AuthenticationResultData",
		},
		Type: "V4/ThreeDSResponse",
	}

	kt := KrTransaction{
		UUID:              tx.UUID,
		Amount:            tx.Amount,
		Currency:          tx.Currency,
		PaymentMethodType: paymentMethodType,
		Status:            spec.TxStatus,
		DetailedStatus:    spec.DetailedStatus,
		OperationType:     spec.OperationType,
		CreationDate:      now,
		ErrorCode:         opts.ErrorCode,
		ErrorMessage:      opts.ErrorMessage,
		Metadata:          tx.Metadata,
		TransactionDetails: KrTransactionDetails{
			CreationContext: "CHARGE",
			Wallet:          opts.Wallet,
			CardDetails:     cardDetails,
			ThreeDSResponse: threeDS,
			Type:            "V4/TransactionDetails",
		},
		Type: "V4/PaymentTransaction",
	}

	// Le montant effectif est le montant capture (0 si non capture).
	// Approximation acceptable en phase 1 : pour AUTHORISED on garde 0
	// (fonds reserves non debites), pour PAID on met le total.
	var effective format.Amount
	if opts.Outcome == OutcomePaid {
		effective = tx.Amount
	}

	return &KrAnswer{
		OrderCycle:         spec.OrderCycle,
		OrderStatus:        spec.OrderStatus,
		ServerDate:         now,
		ServerURL:          serverURL,
		ApplicationVersion: applicationVersion,
		Mode:               mode,
		OrderDetails: KrOrderDetails{
			OrderTotalAmount:     tx.Amount,
			OrderCurrency:        tx.Currency,
			Mode:                 mode,
			OrderID:              tx.OrderID,
			OrderEffectiveAmount: effective,
			Type:                 "V4/OrderDetails",
		},
		Customer:     tx.Customer,
		Transactions: []KrTransaction{kt},
		Type:         "V4/Payment",
	}
}

// BrowserReturnOpts regroupe les parametres de simulation communs a
// browserReturn et ipn — permet de partager buildKrAnswer sans
// dupliquer le decodage cote handler.
type BrowserReturnOpts struct {
	Outcome           string
	PaymentMethodType string
	CardBrand         string
	Wallet            string
	ThreeDSStatus     string
	ErrorCode         string
	ErrorMessage      string
}

// buildDeliveryWebhook construit le Webhook a remettre a
// internal/delivery : URL cible, corps form-urlencoded avec les cinq
// champs kr-* attendus par un integrateur PayZen, signature calculee
// via Sign. Prend un id de webhook explicite pour permettre au handler
// de le retourner au marchand pour tracage.
func buildDeliveryWebhook(id, targetURL string, answer *KrAnswer, hmacKey, answerType string) (delivery.Webhook, string, error) {
	raw, err := json.Marshal(answer)
	if err != nil {
		return delivery.Webhook{}, "", fmt.Errorf("serialisation kr-answer: %w", err)
	}
	hash := Sign(raw, hmacKey)

	form := url.Values{}
	form.Set("kr-answer", string(raw))
	form.Set("kr-hash", hash)
	form.Set("kr-hash-algorithm", "sha256_hmac")
	form.Set("kr-hash-key", "sha256_hmac")
	form.Set("kr-answer-type", answerType)

	wh := delivery.Webhook{
		ID:  id,
		URL: targetURL,
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: []byte(form.Encode()),
	}
	return wh, hash, nil
}

