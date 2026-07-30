// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package domain

import "errors"

// Sentinelles exportées : l'appelant doit pouvoir distinguer trois familles
// de refus. Une transition interdite (état source incompatible) n'a pas la
// même correction qu'un montant hors bornes (nul, négatif, ou remboursement
// dépassant le capturé), qui n'a rien à voir non plus avec des identifiants
// mal formés à la création. La distinction sert d'abord aux appelants (API
// de contrôle, tests, journaux), pas à l'utilisateur final.
var (
	// ErrInvalidTransition indique que la méthode appelée n'est pas autorisée
	// depuis l'état courant du paiement.
	ErrInvalidTransition = errors.New("transition d'etat invalide")

	// ErrInvalidAmount indique un montant nul, négatif, ou un cumul de
	// remboursements qui dépasserait le montant capturé.
	ErrInvalidAmount = errors.New("montant invalide")

	// ErrInvalidPayment indique un identifiant de paiement absent ou mal formé.
	ErrInvalidPayment = errors.New("paiement invalide")

	// ErrInvalidCurrency indique un code devise qui n'est pas un triplet de
	// lettres majuscules ASCII (contrat ISO 4217, l'existence effective de la
	// devise n'est pas vérifiée par le domaine).
	ErrInvalidCurrency = errors.New("devise invalide")
)
