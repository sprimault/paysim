// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
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

// knownOutcomes liste les outcomes acceptés, triés pour que le message
// d'erreur reste stable d'un appel à l'autre — un ordre de map rendrait
// deux réponses identiques textuellement différentes.
func knownOutcomes() []string {
	out := make([]string, 0, len(outcomeSpecs))
	for k := range outcomeSpecs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

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
// pure : reçoit un Transaction (avec Payment déjà transité), le moyen
// de paiement enregistré s'il en existe un, et les options de la
// requête de simulation. Retourne la structure JSON à signer. Ne
// modifie rien, n'écrit pas.
//
// pm porte la carte réellement enrôlée : quand il est non-nil, le bloc
// cardDetails en est dérivé plutôt que fabriqué. Le laisser nil est le
// cas légitime du paiement one-shot où aucune carte n'a été saisie —
// on retombe alors sur une carte de démonstration.
//
// Défauts appliqués : paymentMethodType=CARDS, cardBrand=VISA,
// threeDSStatus=SUCCESS, authenticationType déduit du status.
func buildKrAnswer(tx *Transaction, pm *PaymentMethod, opts BrowserReturnOpts, serverURL string, mode string) *KrAnswer {
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
		// Sans carte enrôlée, on annonce une carte de démonstration :
		// il n'y a rien de réel à décrire. Dès qu'il y en a une, tout
		// vient d'elle — annoncer un PAN ou une expiration qui diverge
		// de ce qu'on stocke ferait mentir le simulateur sur ses
		// propres données, et casserait tout test marchand portant sur
		// les quatre derniers chiffres ou sur la date de péremption.
		pan := newMaskedPAN(cardBrand)
		expiryMonth := 12
		expiryYear := time.Now().UTC().Year() + 2
		holderName := ""

		// Ces trois-là restent des défauts, mais des défauts qu'une
		// carte peut désormais contredire : sans ça, ni carte
		// étrangère, ni carte de débit, ni routage par émetteur
		// n'étaient simulables.
		country := "FR"
		productCategory := "CREDIT"
		issuerName := "PAYSIM"

		if pm != nil {
			pan = pm.PANMasked
			expiryMonth = pm.ExpiryMonth
			expiryYear = pm.ExpiryYear
			holderName = pm.HolderName
			if pm.Brand != "" {
				cardBrand = pm.Brand
			}
			if pm.Country != "" {
				country = pm.Country
			}
			if pm.ProductCategory != "" {
				productCategory = pm.ProductCategory
			}
			if pm.IssuerName != "" {
				issuerName = pm.IssuerName
			}
		}
		cardDetails = &KrCardDetails{
			PAN:             pan,
			Brand:           cardBrand,
			HolderName:      holderName,
			ProductCategory: productCategory,
			ExpiryMonth:     expiryMonth,
			ExpiryYear:      expiryYear,
			Country:         country,
			IssuerName:      issuerName,
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
		UUID:               tx.UUID,
		Amount:             tx.Amount,
		Currency:           tx.Currency,
		PaymentMethodType:  paymentMethodType,
		PaymentMethodToken: tx.PaymentMethodToken, // propagé si enrôlement ou rejeu
		Status:             spec.TxStatus,
		DetailedStatus:     spec.DetailedStatus,
		OperationType:      spec.OperationType,
		CreationDate:       now,
		ErrorCode:          opts.ErrorCode,
		ErrorMessage:       opts.ErrorMessage,
		Metadata:           tx.Metadata,
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
	Chaos             WebhookChaos
	DeliveryDelayMs   int
}

// buildDeliveryWebhook construit le Webhook a remettre a
// internal/delivery : URL cible, corps form-urlencoded avec les cinq
// champs kr-* attendus par un integrateur PayZen, signature calculee
// via Sign. Prend un id de webhook explicite pour permettre au handler
// de le retourner au marchand pour tracage.
//
// badSignature altere le kr-hash envoye (flip du premier caractere
// hex) tout en preservant sa forme (64 chars hex minuscule). Le hash
// retourne comme deuxieme valeur reste le VRAI hash, pour que le
// handler puisse le retourner au marchand a titre diagnostique — il
// verra ainsi que ce qu'il recoit ne correspond pas.
//
// delay est propage au Webhook.Delay, respecte par le scheduler avant
// l'envoi effectif — permet le out-of-order par composition.
// L'outcome porté par le Webhook vient de answer.OrderStatus : c'est
// l'adaptateur qui traduit son protocole en résultat métier, delivery
// ne lit jamais le corps.
func buildDeliveryWebhook(id, targetURL string, answer *KrAnswer, hmacKey, answerType string, badSignature bool, delay time.Duration) (delivery.Webhook, string, error) {
	raw, err := json.Marshal(answer)
	if err != nil {
		return delivery.Webhook{}, "", fmt.Errorf("serialisation kr-answer: %w", err)
	}
	hash := Sign(raw, hmacKey)

	sentHash := hash
	if badSignature {
		sentHash = flipFirstHexChar(hash)
	}

	form := url.Values{}
	form.Set("kr-answer", string(raw))
	form.Set("kr-hash", sentHash)
	form.Set("kr-hash-algorithm", "sha256_hmac")
	form.Set("kr-hash-key", "sha256_hmac")
	form.Set("kr-answer-type", answerType)

	wh := delivery.Webhook{
		ID:  id,
		URL: targetURL,
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body:    []byte(form.Encode()),
		Outcome: answer.OrderStatus,
		Delay:   delay,
	}
	return wh, hash, nil
}

// flipFirstHexChar altere un hash hex en changeant son premier
// caractere : '0' devient '1', tout autre chiffre devient '0'.
// Preserve la longueur et le format — teste la verification chez le
// marchand, pas un check naif de forme.
func flipFirstHexChar(s string) string {
	if len(s) == 0 {
		return s
	}
	b := []byte(s)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}

