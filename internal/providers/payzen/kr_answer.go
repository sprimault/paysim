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

	"github.com/sprimault/paysim/internal/chaos"
	"github.com/sprimault/paysim/internal/clock"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/format"
)

// applicationVersion est la version du serveur PayZen simulé —
// arbitraire, cohérente avec ce qu'un vrai back-office annoncerait.
const applicationVersion = "6.0.0-paysim"

// algorithmeSignature est la seule valeur que la plateforme émet, et la
// seule que les SDK marchands acceptent.
const algorithmeSignature = "sha256_hmac"

// algorithmeInattendu est annoncé par le mode de chaos bad-algorithm.
// Choisi plausible plutôt qu'absurde : un SDK doit le rejeter parce
// qu'il ne le supporte pas, pas parce que la valeur est manifestement
// fabriquée.
const algorithmeInattendu = "sha512_hmac"

// codesMarque associe chaque marque Lyra au code qu'elle annonce dans
// applicationProvider. Valeurs relevées sur les API de production en
// août 2026 : elles ne se déduisent d'aucun nom de domaine, et quatre
// hôtes distincts partagent PAYZEN — d'où une table explicite plutôt
// qu'une règle.
var codesMarque = map[string]string{
	"payzen":       "PAYZEN",
	"systempay":    "NPS",
	"sogecommerce": "SOGECOM",
	"scellius":     "LBP",
	"lyra":         "LYRA",
}

// codeMarque rend le code d'enveloppe d'une marque. Une marque inconnue
// rend une chaîne vide, donc un champ omis : mieux vaut ne rien annoncer
// qu'annoncer la mauvaise marque.
func codeMarque(brand string) string {
	return codesMarque[marqueOuDefaut(brand)]
}

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

// errorCodeFor retourne le code PSP de la transaction : celui fourni
// par l'appelant s'il y en a un, sinon PAYSIM_REFUSED sur un refus.
//
// Centralisé ici plutôt qu'à chaque site d'émission : un refus sans code
// obligerait le marchand à deviner qu'il en est un, alors que le
// protocole lui promet un code.
func errorCodeFor(opts BrowserReturnOpts) string {
	if opts.ErrorCode != "" {
		return opts.ErrorCode
	}
	if opts.Outcome == OutcomeUnpaid {
		return ErrCodeRefused
	}
	return ""
}

// tokenStatus qualifie l'alias annoncé : ACTIVE, ou CANCELLED s'il a
// été résilié.
//
// Rien sans token — un statut seul ne qualifierait rien. Et rien non
// plus quand le moyen n'a pas pu être relu : mieux vaut un champ absent
// qu'un « ACTIVE » affirmé sans l'avoir vérifié.
func tokenStatus(token string, pm *PaymentMethod) string {
	if token == "" || pm == nil {
		return ""
	}
	if pm.Revoked {
		return "CANCELLED"
	}
	return "ACTIVE"
}

// declineNote compose le motif inscrit au journal du domaine : le texte
// en clair, suivi du code bancaire quand il y en a un.
//
// Le journal est ce qu'on lit dans l'interface pour comprendre un refus.
// Y porter le code évite d'avoir à rouvrir le kr-answer brut — c'est
// exactement le détour qu'on cherche à supprimer.
func declineNote(reason string, d chaos.DeclineReason) string {
	switch {
	case d.Code == "":
		return reason
	case reason == "":
		return fmt.Sprintf("%s (%s)", d.Message, d.Code)
	default:
		return fmt.Sprintf("%s (%s %s)", reason, d.Code, d.Message)
	}
}

// applyOutcome fait progresser le domain.Payment de la transaction
// selon l'outcome demandé. Miroir des transitions autorisées par le
// domaine — toute divergence produit une erreur domain (par exemple
// PAID sur un paiement déjà capturé). Le mapping ABANDONED→Expire est
// documenté dans les constantes.
//
// Le motif bancaire est écrit ici, et non chez les appelants : quatre
// chemins mènent à un refus, et un motif renseigné dans trois d'entre
// eux ne se voit pas — il produit simplement une interface muette sur le
// quatrième. La note du journal et les champs structurés partent donc du
// même endroit, de la même source.
func applyOutcome(tx *Transaction, outcome, reason string, decline chaos.DeclineReason) error {
	switch outcome {
	case OutcomePaid:
		return tx.Payment.Capture()
	case OutcomeAuthorised:
		return tx.Payment.Authorize()
	case OutcomeUnpaid:
		tx.DeclineCode = decline.Code
		tx.DeclineMessage = decline.Message
		note := declineNote(reason, decline)
		if note == "" {
			note = "simulation"
		}
		return tx.Payment.Decline(note)
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
// pm porte la carte réellement enrôlée, presentee celle qui a été
// soumise sans l'être — le cas d'un refus, où aucun alias ne naît. Le
// bloc cardDetails dérive de l'un ou de l'autre.
//
// La carte de démonstration ne sert que lorsque les deux sont nils,
// c'est-à-dire quand rien n'a jamais été présenté : un paiement joué
// depuis l'UI sans qu'aucun numéro n'ait été saisi. Annoncer une carte
// fictive alors qu'on connaît celle qui a été refusée ferait journaliser
// au marchand un PAN masqué qui n'a jamais existé — et le refus est
// précisément le scénario pour lequel on installe ce simulateur.
//
// Défauts appliqués : paymentMethodType=CARDS, cardBrand=VISA,
// threeDSStatus=SUCCESS, authenticationType déduit du status.
func buildKrAnswer(clk clock.Clock, tx *Transaction, pm *PaymentMethod, presentee *Card, opts BrowserReturnOpts, serverURL string, mode string) *KrAnswer {
	spec := outcomeSpecs[opts.Outcome]
	now := clk.Now().Format(time.RFC3339)

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
		expiryYear := clk.Now().Year() + 2
		holderName := ""

		// Ces trois-là restent des défauts, mais des défauts qu'une
		// carte peut désormais contredire : sans ça, ni carte
		// étrangère, ni carte de débit, ni routage par émetteur
		// n'étaient simulables.
		country := "FR"
		productCategory := "CREDIT"
		issuerName := "PAYSIM"

		switch {
		case pm != nil:
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
		case presentee != nil:
			// Refusée, donc jamais enrôlée : le masquage se fait ici, à
			// partir du numéro soumis. C'est la même règle que celle
			// appliquée à l'enrôlement, pour que le marchand lise les
			// mêmes quatre derniers chiffres dans les deux cas.
			pan = maskPAN(presentee.PAN)
			expiryMonth = presentee.ExpiryMonth
			expiryYear = presentee.ExpiryYear
			holderName = presentee.HolderName
			if presentee.Brand != "" {
				cardBrand = presentee.Brand
			}
			if presentee.Country != "" {
				country = presentee.Country
			}
			if presentee.ProductCategory != "" {
				productCategory = presentee.ProductCategory
			}
			if presentee.IssuerName != "" {
				issuerName = presentee.IssuerName
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
		UUID:                     tx.UUID,
		Amount:                   tx.Amount,
		Currency:                 tx.Currency,
		PaymentMethodType:        paymentMethodType,
		PaymentMethodToken:       tx.PaymentMethodToken, // propagé si enrôlement ou rejeu
		PaymentMethodTokenStatus: tokenStatus(tx.PaymentMethodToken, pm),
		Status:                   spec.TxStatus,
		DetailedStatus:           spec.DetailedStatus,
		OperationType:            spec.OperationType,
		CreationDate:             now,
		ErrorCode:                errorCodeFor(opts),
		ErrorMessage:             opts.ErrorMessage,

		DetailedErrorCode:    opts.DeclineReason.Code,
		DetailedErrorMessage: opts.DeclineReason.Message,
		Metadata:             tx.Metadata,
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
		OrderCycle:          spec.OrderCycle,
		OrderStatus:         spec.OrderStatus,
		ServerDate:          now,
		ServerURL:           serverURL,
		ApplicationVersion:  applicationVersion,
		ApplicationProvider: codeMarque(tx.Brand),
		Mode:                mode,
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
	// Outcome est l'issue à jouer : PAID, AUTHORISED, UNPAID, EXPIRED
	// ou ABANDONED. C'est elle qui pilote toute la table outcomeSpecs.
	Outcome string

	// PaymentMethodType et CardBrand décrivent le moyen annoncé.
	// Ignorés dès qu'un moyen enrôlé accompagne la transaction : ce
	// qu'on annonce vient alors de la carte réelle.
	PaymentMethodType string
	CardBrand         string

	// Wallet nomme le portefeuille employé (APPLE_PAY, GOOGLEPAY).
	Wallet string

	// ThreeDSStatus est le verdict d'authentification annoncé —
	// SUCCESS, CHALLENGE ou FAILURE. Il détermine aussi
	// authenticationType dans le webhook.
	ThreeDSStatus string

	// ErrorCode et ErrorMessage habillent un refus côté PSP.
	ErrorCode    string
	ErrorMessage string

	// DeclineReason porte le motif bancaire du refus — le code ISO 8583
	// sur lequel un marchand décide de retenter ou de réclamer une autre
	// carte. Vide sur un succès, ou sur un refus sans motif bancaire
	// (abandon, expiration).
	DeclineReason chaos.DeclineReason

	// Chaos et DeliveryDelayMs portent l'injection de panne sur cette
	// livraison. Inertes par défaut : le chaos ne s'active jamais tout
	// seul.
	Chaos           WebhookChaos
	DeliveryDelayMs int
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
func buildDeliveryWebhook(id, targetURL string, answer *KrAnswer, cle, nomCle, answerType string, alterations WebhookChaos, delay time.Duration) (delivery.Webhook, string, error) {
	raw, err := json.Marshal(answer)
	if err != nil {
		return delivery.Webhook{}, "", fmt.Errorf("serialisation kr-answer: %w", err)
	}
	hash := Sign(raw, cle)

	sentHash := hash
	if alterations.BadSignature {
		sentHash = flipFirstHexChar(hash)
	}

	// Le hash reste valide quand seul l'algorithme est faussé : c'est
	// une panne différente d'une signature altérée. Le SDK marchand
	// n'arrive jamais à la comparaison — il lève sur l'algorithme
	// inconnu, et c'est cette branche-là qu'on veut faire tomber.
	algorithme := algorithmeSignature
	if alterations.BadAlgorithm {
		algorithme = algorithmeInattendu
	}

	form := url.Values{}
	form.Set("kr-answer", string(raw))
	form.Set("kr-hash", sentHash)
	form.Set("kr-hash-algorithm", algorithme)
	// kr-hash-key nomme la clé qui a servi, il ne décrit pas
	// l'algorithme : « sha256_hmac » pour le retour navigateur,
	// « password » pour la notification serveur. Le SDK marchand s'en
	// sert pour choisir laquelle de ses deux clés appliquer.
	form.Set("kr-hash-key", nomCle)
	form.Set("kr-answer-type", answerType)

	// Le paiement rattaché se lit dans la réponse que cet adaptateur
	// vient de construire : buildKrAnswer pose KrTransaction.UUID à
	// partir du paiement du domaine, les deux identifiants sont donc le
	// même. Le déduire ici évite de faire descendre un paramètre de plus
	// dans les quatre appelants, et garantit que le webhook est rattaché
	// au paiement qu'il annonce — pas à celui qu'un appelant croyait
	// passer.
	var paymentUUID string
	if len(answer.Transactions) > 0 {
		paymentUUID = answer.Transactions[0].UUID
	}

	wh := delivery.Webhook{
		ID:  id,
		URL: targetURL,
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body:        []byte(form.Encode()),
		Outcome:     answer.OrderStatus,
		PaymentUUID: paymentUUID,
		Delay:       delay,
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
