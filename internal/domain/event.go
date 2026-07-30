// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"time"

	"github.com/sprimault/paysim/internal/format"
)

// EventKind est le type d'entrée inscrite au journal d'un paiement.
type EventKind string

// Valeurs possibles d'EventKind. Chaque transition d'état produit exactement
// un événement, plus un événement à la création. Un remboursement partiel
// supplémentaire (partially_refunded → partially_refunded) produit aussi un
// événement, même si l'état ne change pas — c'est l'événement, pas le
// changement d'état, qui alimente la chronologie affichée en phase 3.
const (
	EventCreated    EventKind = "created"
	EventAuthorized EventKind = "authorized"
	EventCaptured   EventKind = "captured"
	EventRefunded   EventKind = "refunded"
	EventDeclined   EventKind = "declined"
	EventExpired    EventKind = "expired"
	EventChargeback EventKind = "chargeback"
)

// Event est une entrée immuable du journal du paiement. Le journal est la
// source de vérité pour reconstruire l'historique complet ; l'état courant du
// paiement, lui, n'en est qu'un résumé.
//
//   - At    : horodatage UTC de l'événement.
//   - Kind  : type d'événement.
//   - Amount: montant pertinent pour cet événement (typiquement un
//     remboursement partiel). Vaut zéro quand l'événement ne porte pas
//     de montant propre.
//   - Note  : texte libre pour les événements qui en portent (raison d'un
//     refus, référence externe d'un chargeback). Vide sinon.
type Event struct {
	At     time.Time
	Kind   EventKind
	Amount format.Amount
	Note   string
}
