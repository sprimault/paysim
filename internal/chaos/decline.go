// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package chaos

import "github.com/sprimault/paysim/internal/format"

// Motifs de refus.
//
// Un marchand ne traite pas tous les refus de la même façon : une
// provision insuffisante se retente dans trois jours, une opposition
// impose de réclamer une nouvelle carte tout de suite. Sans motif dans
// la réponse, cette logique de reconduction ne peut ni s'écrire ni se
// tester — elle part en production à l'aveugle, et se découvre par un
// client suspendu à tort.
//
// Les codes sont ceux de l'ISO 8583, la norme des messages
// d'autorisation : ce ne sont pas des valeurs PayZen, ce sont celles que
// l'acquéreur remonte et que PayZen relaie tel quel dans
// detailedErrorCode. Les reproduire à l'identique est le seul moyen
// qu'un mapping écrit contre Paysim reste valable en production.
//
// Paysim n'interprète jamais ces codes et n'expose aucun booléen
// « rejouable » : décider qu'un 51 se retente et qu'un 43 ne se retente
// pas est un choix marchand, pas une donnée du protocole. Ajouter ce
// verdict reviendrait à inventer une sémantique que le vrai n'a pas.

// DeclineReason décrit pourquoi un paiement a été refusé.
type DeclineReason struct {
	// Code est le code de retour d'autorisation ISO 8583, tel que la
	// banque le renvoie : 51, 43, 91… Vide si le refus n'a pas de motif
	// bancaire (abandon, expiration).
	Code string

	// Message est le libellé associé, en clair.
	Message string
}

// Motifs reconnus, choisis pour couvrir les trois comportements
// marchands distincts plutôt que pour être exhaustifs.
var (
	// ReasonInsufficientFunds — le solde peut se reconstituer, d'où une
	// reconduction différée qui a des chances d'aboutir.
	ReasonInsufficientFunds = DeclineReason{Code: "51", Message: "provision insuffisante"}

	// ReasonStolenCard — opposition. Retenter est inutile et suspect :
	// il faut réclamer un autre moyen de paiement.
	ReasonStolenCard = DeclineReason{Code: "43", Message: "carte volee, opposition"}

	// ReasonIssuerUnavailable — panne côté émetteur, sans rapport avec
	// le porteur ni la carte. Se retente vite, contrairement au 51.
	ReasonIssuerUnavailable = DeclineReason{Code: "91", Message: "emetteur inaccessible"}

	// ReasonDoNotHonour — refus générique de l'émetteur, sans motif
	// communiqué. Le cas le plus fréquent en production, et le plus
	// ingrat : rien n'indique s'il faut retenter.
	ReasonDoNotHonour = DeclineReason{Code: "05", Message: "refus de l'emetteur"}

	// ReasonNotPermitted — opération interdite à ce porteur (plafond de
	// type d'opération, carte non autorisée à l'international…).
	ReasonNotPermitted = DeclineReason{Code: "57", Message: "operation non permise a ce porteur"}
)

// MagicDeclineReason retourne le motif associé à un montant magique, ou
// un motif vide si le montant n'en porte aucun.
//
// La convention suit celle de MagicOutcome, dont elle qualifie le refus :
//
//   - centimes 01 → 51, provision insuffisante (rejouable plus tard)
//   - centimes 02 → 43, carte volée (définitif, nouvelle carte)
//   - centimes 04 → 91, émetteur inaccessible (transitoire)
//
// Les centimes 03 restent pris par la latence, cf. MagicLatencyMs.
func MagicDeclineReason(amount format.Amount) DeclineReason {
	switch amount % 100 {
	case 1:
		return ReasonInsufficientFunds
	case 2:
		return ReasonStolenCard
	case 4:
		return ReasonIssuerUnavailable
	}
	return DeclineReason{}
}

// declineReasonByPAN associe un motif fixe à chaque PAN de test refusé.
//
// Le montant magique ne suffit pas pour un prélèvement récurrent : son
// montant est imposé par l'abonnement, on ne peut pas le tordre pour
// obtenir un motif. Le PAN, lui, est choisi à l'enrôlement et reste
// stable — c'est le seul levier disponible sur ce chemin.
var declineReasonByPAN = map[string]DeclineReason{
	"4000000000000002": ReasonInsufficientFunds,
	"5105105105105100": ReasonStolenCard,
	"2223000000000007": ReasonDoNotHonour,
	"378282000000008":  ReasonNotPermitted,
}

// DeclineReasonForPAN retourne le motif fixé pour un PAN de test, ou un
// motif vide si le PAN n'en est pas un.
func DeclineReasonForPAN(pan string) DeclineReason {
	return declineReasonByPAN[pan]
}
