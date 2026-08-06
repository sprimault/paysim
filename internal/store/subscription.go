// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"time"

	"github.com/sprimault/paysim/internal/format"
)

// SubscriptionRecord est la représentation persistante d'un abonnement,
// commune à tous les providers. Même philosophie que PaymentRecord :
// core queryable (ID, Provider, Amount, PaymentMethodToken), spécifique
// dans ProviderDataJSON.
//
// EffectDate et Rrule sont gardés en string plutôt que parsés :
// EffectDate suit ISO 8601 tel que PayZen le renvoie et n'a pas besoin
// d'arithmétique côté store ; Rrule est du RFC 5545 iCalendar
// (`RRULE:FREQ=MONTHLY;INTERVAL=1`) — parsing spécialisé, hors périmètre
// d'un repo générique.
type SubscriptionRecord struct {
	// ID est l'identifiant unique cross-provider (subscriptionId chez
	// PayZen). Clé primaire.
	ID string

	// Provider identifie l'adaptateur ("payzen", "stripe", ...).
	Provider string

	// OrderID est la référence marchand — libre côté marchand.
	OrderID string

	// Amount en centimes de la devise Currency.
	Amount format.Amount

	// Currency ISO 4217.
	Currency string

	// PaymentMethodToken pointe le moyen de paiement enregistré qui
	// portera les échéances de cet abonnement. Ce token doit exister
	// dans PaymentMethodRepository — pas de contrainte FK cross-repos,
	// vérifié à l'usage.
	PaymentMethodToken string

	// EffectDate : première échéance, format ISO 8601.
	EffectDate string

	// Rrule : règle de récurrence RFC 5545 iCalendar.
	Rrule string

	// MetadataJSON sérialise la map[string]string libre du marchand.
	MetadataJSON string

	// ProviderDataJSON porte les champs spécifiques provider.
	ProviderDataJSON string

	// Cancelled : true après une annulation manuelle. Un renewal
	// ultérieur est refusé (mécanique symétrique à
	// PaymentMethodRecord.Revoked).
	Cancelled bool

	// CreatedAt / UpdatedAt : timestamps de l'enveloppe subscription
	// (pas des renewals individuels, qui sont des paiements distincts).
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SubscriptionRepository est le contrat de persistance des abonnements,
// symétrique à PaymentRepository. Toute impl (SQLite, ...) le respecte.
// Les erreurs remontent telles quelles.
type SubscriptionRepository interface {
	// Save insère ou met à jour un enregistrement. Idempotent sur
	// même ID.
	Save(rec *SubscriptionRecord) error

	// ByID retourne l'abonnement identifié, ou nil, nil si inconnu.
	ByID(id string) (*SubscriptionRecord, error)

	// ByProvider retourne tous les abonnements d'un provider, ordonnés
	// par UpdatedAt décroissant.
	ByProvider(provider string) ([]*SubscriptionRecord, error)

	// Count retourne le nombre total d'abonnements. Cross-provider.
	Count() (int, error)

	// DeleteByID supprime un abonnement. Idempotent : silencieusement
	// no-op si l'ID est inconnu.
	DeleteByID(id string) error

	// DeleteByProvider supprime tous les abonnements d'un provider.
	// Retourne le nombre effectivement supprimé.
	DeleteByProvider(provider string) (int, error)

	// DeleteAll supprime tous les abonnements, quel que soit le
	// provider. Retourne le nombre supprimé.
	DeleteAll() (int, error)

	// Cancel marque l'abonnement comme annulé. Idempotent : ID inconnu
	// ne remonte pas d'erreur (l'état demandé « abonnement annulé »
	// est atteint pour un ID inexistant).
	Cancel(id string) error

	// Close libère les ressources sous-jacentes.
	Close() error
}
