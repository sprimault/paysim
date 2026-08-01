// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package store définit les contrats de persistance génériques,
// partagés entre tous les providers PSP. Les colonnes typées sont
// normalisées (query-able cross-provider) — state, amount, provider,
// timestamps, order_id. Les champs spécifiques provider (Customer,
// ReturnURL, kr-answer type…) vivent dans des blobs JSON opaques que
// chaque adaptateur (payzen, stripe, …) sérialise/désérialise via ses
// propres converters.
//
// Une seule table SQL persiste tous les paiements, quel que soit le
// provider : ça permet des requêtes cross-provider naturelles
// (« tous les paiements PAID des dernières 24h ») sans compromis
// forcés sur la modélisation. Les champs spécifiques n'apparaissent
// jamais comme colonnes normalisées : on refuse la fausse
// normalisation qui aligne des concepts qui n'ont rien à voir.
package store

import (
	"time"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
)

// PaymentRecord est la représentation persistante d'un paiement,
// commune à tous les providers. Le core (UUID, Provider, montants,
// state) est queryable directement ; le reste (Customer, ProviderData)
// vit en blobs JSON opaques.
//
// Events est chargé/sauvegardé conjointement — la persistence est
// atomique côté SQLite (INSERT payment + DELETE/INSERT events dans
// la même transaction).
type PaymentRecord struct {
	// UUID est l'identifiant unique cross-provider.
	UUID string

	// Provider identifie l'adaptateur ("payzen", "stripe", ...).
	Provider string

	// ProviderRef est le token principal du provider — le formToken
	// PayZen ou le payment_intent_id Stripe. Indexé avec Provider pour
	// permettre les lookups par ce champ.
	ProviderRef string

	// OrderID est la référence marchand — libre côté marchand,
	// indexée côté store pour les recherches.
	OrderID string

	// Amount en centimes, entier positif.
	Amount format.Amount

	// Currency ISO 4217 (trois lettres majuscules).
	Currency string

	// State reflète domain.Payment.State() au moment de la sauvegarde.
	State domain.State

	// Refunded : cumul des remboursements appliqués.
	Refunded format.Amount

	// CustomerJSON sérialise la struct Customer spécifique au provider
	// (PayZen : email + BillingDetails, Stripe : id + phone + address,
	// etc.). Le core store ne l'interprète pas.
	CustomerJSON string

	// MetadataJSON sérialise la map[string]string libre du marchand —
	// commune entre providers.
	MetadataJSON string

	// ProviderDataJSON porte tout le reste spécifique au provider :
	// URL de retour PayZen, capture_method Stripe, etc.
	ProviderDataJSON string

	// Events est le journal complet du paiement — copié depuis
	// domain.Payment.Events() au moment du Save.
	Events []domain.Event

	// CreatedAt / UpdatedAt sont les timestamps de la Transaction
	// enveloppe (pas ceux du domain.Payment) — quand le contexte a
	// été créé chez le provider et sa dernière évolution.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PaymentRepository est le contrat de persistance des paiements. Toute
// impl (mémoire, SQLite, ...) le respecte. Les erreurs remontent
// telles quelles à l'appelant — la mémoire ne peut pas échouer, la
// signature reste identique pour la symétrie.
type PaymentRepository interface {
	// Save insère ou met à jour un enregistrement complet, avec ses
	// events. L'opération est atomique — soit tout est écrit, soit
	// rien ne change.
	Save(rec *PaymentRecord) error

	// ByUUID retourne le paiement identifié par UUID, ou nil, nil si
	// inconnu.
	ByUUID(uuid string) (*PaymentRecord, error)

	// ByProviderRef retourne le paiement d'un provider donné identifié
	// par son ProviderRef (formToken, pi_xxx, ...). Nil, nil si
	// inconnu. Le couple (provider, providerRef) est unique.
	ByProviderRef(provider, providerRef string) (*PaymentRecord, error)

	// All retourne tous les paiements, ordonnés par UpdatedAt
	// décroissant (plus récent d'abord). Cross-provider par défaut ;
	// utiliser ByProvider pour filtrer.
	All() ([]*PaymentRecord, error)

	// ByProvider filtre All sur un provider spécifique.
	ByProvider(provider string) ([]*PaymentRecord, error)

	// Count retourne le nombre total de paiements. Cross-provider.
	Count() (int, error)

	// DeleteByUUID supprime un paiement (et ses events en cascade).
	// Silencieusement no-op si l'UUID est inconnu — cohérent avec
	// une opération idempotente ; l'appelant qui veut différencier
	// « supprimé » de « inexistant » doit ByUUID avant.
	DeleteByUUID(uuid string) error

	// DeleteByProvider supprime tous les paiements d'un provider.
	// Retourne le nombre effectivement supprimé pour l'observabilité.
	DeleteByProvider(provider string) (int, error)

	// DeleteAll supprime tous les paiements, quel que soit le provider.
	// Retourne le nombre supprimé.
	DeleteAll() (int, error)

	// Close libère les ressources sous-jacentes (SQLite : ferme le
	// fichier, checkpoint WAL). No-op pour l'impl mémoire.
	Close() error
}
