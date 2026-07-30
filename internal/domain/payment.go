// Copyright (c) 2026 Paysim by Stéphane Primault — Tous droits réservés.
// Author: Stéphane Primault <sprimault@users.noreply.github.com>

// Package domain contient la machine à états du paiement et son journal
// d'événements. Il ne connaît aucun fournisseur — cet invariant est vérifié
// par internal/arch et rend le moteur de chaos uniforme (toute anomalie est
// injectée en amont, dans internal/chaos et internal/delivery, jamais dans un
// adaptateur).
package domain

import (
	"time"

	"github.com/sprimault/paysim/internal/format"
)

// Payment est l'agrégat racine du domaine. Ses champs sont privés :
// toute mutation passe par les méthodes typées ci-dessous, qui garantissent
// la cohérence de la machine à états et alimentent le journal d'événements.
//
// Payment n'est pas synchronisé. La sérialisation des accès concurrents est
// de la responsabilité du store (internal/store) qui détiendra l'instance
// unique — voir l'invariant 8 (une seule réplique de Paysim).
type Payment struct {
	id        string
	amount    format.Amount // total demandé, en centimes
	currency  string        // ISO 4217, trois lettres majuscules
	state     State
	refunded  format.Amount // cumul des remboursements déjà appliqués
	events    []Event
	createdAt time.Time
	updatedAt time.Time
}

// New instancie un paiement dans l'état initié et enregistre l'événement de
// création. Le montant doit être strictement positif ; la devise doit avoir
// la forme d'un code ISO 4217 (trois lettres majuscules ASCII) — l'existence
// effective du code n'est pas vérifiée, c'est le rôle de la couche d'entrée.
func New(id string, amount format.Amount, currency string) (*Payment, error) {
	if id == "" {
		return nil, ErrInvalidPayment
	}
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if !isCurrencyCode(currency) {
		return nil, ErrInvalidCurrency
	}
	p := &Payment{
		id:       id,
		amount:   amount,
		currency: currency,
		state:    StateInitiated,
	}
	p.record(EventCreated, 0, "")
	// createdAt et updatedAt partagent le timestamp du premier événement,
	// pour que la chronologie soit strictement monotone dès le départ.
	p.createdAt = p.updatedAt
	return p, nil
}

// ID retourne l'identifiant du paiement.
func (p *Payment) ID() string { return p.id }

// Amount retourne le montant total demandé, en centimes.
func (p *Payment) Amount() format.Amount { return p.amount }

// Currency retourne le code devise ISO 4217.
func (p *Payment) Currency() string { return p.currency }

// State retourne l'état courant.
func (p *Payment) State() State { return p.state }

// Refunded retourne le cumul déjà remboursé, en centimes. Vaut zéro tant
// qu'aucun remboursement n'a eu lieu, et Amount() une fois le paiement
// intégralement remboursé.
func (p *Payment) Refunded() format.Amount { return p.refunded }

// CreatedAt retourne l'horodatage UTC de création.
func (p *Payment) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt retourne l'horodatage UTC du dernier événement enregistré.
func (p *Payment) UpdatedAt() time.Time { return p.updatedAt }

// Events retourne une copie du journal. La copie garantit qu'un appelant ne
// peut pas modifier l'historique interne — condition d'immuabilité du journal.
// Le coût mémoire est trivial ; si un jour il devient sensible, on exposera
// un itérateur en range-over-func plutôt que d'ouvrir la slice interne.
func (p *Payment) Events() []Event {
	out := make([]Event, len(p.events))
	copy(out, p.events)
	return out
}

// Authorize passe le paiement en état autorisé — les fonds sont réservés
// mais non débités. C'est le mode « 3DS + capture différée », par opposition
// à la capture immédiate (Capture depuis initiated).
func (p *Payment) Authorize() error {
	if p.state != StateInitiated {
		return ErrInvalidTransition
	}
	p.state = StateAuthorized
	p.record(EventAuthorized, 0, "")
	return nil
}

// Capture débite les fonds. Valide depuis initiated (mode « vente »,
// autorisation + capture atomiques) ou authorized (capture postérieure à une
// autorisation séparée). La capture est toujours totale — la capture partielle
// n'est pas modélisée en phase 0.
func (p *Payment) Capture() error {
	if p.state != StateInitiated && p.state != StateAuthorized {
		return ErrInvalidTransition
	}
	p.state = StateCaptured
	p.record(EventCaptured, 0, "")
	return nil
}

// Refund rembourse tout ou partie du montant capturé. Plusieurs remboursements
// partiels successifs sont acceptés tant que leur cumul ne dépasse pas le
// montant total. L'état passe à refunded (terminal) quand le cumul atteint
// exactement le total ; sinon il devient — ou reste — partially_refunded.
//
// Un second remboursement partiel depuis partially_refunded est une
// auto-transition d'état (l'état ne change pas) mais enregistre bien un
// événement au journal, car c'est l'événement qui alimente la chronologie.
func (p *Payment) Refund(amount format.Amount) error {
	if p.state != StateCaptured && p.state != StatePartiallyRefunded {
		return ErrInvalidTransition
	}
	if amount <= 0 || p.refunded+amount > p.amount {
		return ErrInvalidAmount
	}
	p.refunded += amount
	if p.refunded == p.amount {
		p.state = StateRefunded
	} else {
		p.state = StatePartiallyRefunded
	}
	p.record(EventRefunded, amount, "")
	return nil
}

// Decline refuse le paiement. Valide depuis initiated (refus banque, 3DS
// échoué, score de risque) ou authorized (autorisation ultérieurement
// annulée avant capture). L'état devient terminal.
func (p *Payment) Decline(reason string) error {
	if p.state != StateInitiated && p.state != StateAuthorized {
		return ErrInvalidTransition
	}
	p.state = StateDeclined
	p.record(EventDeclined, 0, reason)
	return nil
}

// Expire clôt le paiement pour dépassement de délai — formulaire non complété
// depuis initiated, capture jamais réalisée depuis authorized (une
// autorisation expire au bout de quelques jours chez la plupart des PSP).
// L'état devient terminal.
func (p *Payment) Expire() error {
	if p.state != StateInitiated && p.state != StateAuthorized {
		return ErrInvalidTransition
	}
	p.state = StateExpired
	p.record(EventExpired, 0, "")
	return nil
}

// Chargeback enregistre une rétrofacturation. Possible depuis tout état où
// des fonds ont été débités : captured, partially_refunded, et — cas
// contre-intuitif — refunded. Un client peut contester même après avoir été
// remboursé (fraude classique consistant à recevoir le remboursement puis à
// déclencher la rétrofacturation pour être payé deux fois). L'état devient
// terminal.
func (p *Payment) Chargeback() error {
	if p.state != StateCaptured && p.state != StateRefunded && p.state != StatePartiallyRefunded {
		return ErrInvalidTransition
	}
	p.state = StateChargeback
	p.record(EventChargeback, 0, "")
	return nil
}

// record ajoute une entrée au journal et met à jour updatedAt. Point unique
// où le temps est lu — les appelants ne prennent jamais time.Now eux-mêmes,
// c'est le seul moyen de garantir que updatedAt et le dernier événement du
// journal partagent bien le même horodatage.
func (p *Payment) record(kind EventKind, amount format.Amount, note string) {
	now := time.Now().UTC()
	p.events = append(p.events, Event{
		At:     now,
		Kind:   kind,
		Amount: amount,
		Note:   note,
	})
	p.updatedAt = now
}

// isCurrencyCode vérifie qu'une chaîne a la forme d'un code ISO 4217 :
// exactement trois lettres majuscules ASCII. Comparer les octets suffit
// puisqu'un code valide est nécessairement ASCII — un caractère non-ASCII
// occuperait plusieurs octets et échouerait déjà à len(s) != 3.
func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}
