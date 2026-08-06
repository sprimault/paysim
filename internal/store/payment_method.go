// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package store

import "time"

// PaymentMethodRecord est la représentation persistante d'un moyen de
// paiement enregistré. Adressable par Token (opaque, généré côté
// provider — paymentMethodToken pour PayZen, pm_xxx pour Stripe).
//
// AVERTISSEMENT : Paysim est un simulateur, aucune protection PCI-DSS.
// PANFull est stocké en clair. À n'utiliser JAMAIS avec de vraies
// cartes en usage — voir la doc install.
type PaymentMethodRecord struct {
	// Token : identifiant unique cross-provider. Clé primaire.
	Token string

	// Provider identifie l'adaptateur ("payzen", "stripe", ...).
	Provider string

	// PANFull : numéro de carte complet, en clair (simulateur).
	PANFull string

	// PANMasked : représentation masquée pour affichage marchand,
	// format "411111XXXXXX1111" (PayZen).
	PANMasked string

	// Brand : VISA, MASTERCARD, CB, AMEX, ...
	Brand string

	// HolderName : nom du porteur tel que saisi au formulaire
	// ("DUPONT JEAN"). Optionnel — un wallet (Apple Pay, Google Pay)
	// n'en transmet pas, et PayZen ne l'impose pas à l'enrôlement.
	HolderName string

	// ExpiryMonth 1-12, ExpiryYear 4 chiffres.
	ExpiryMonth int
	ExpiryYear  int

	// Country (ISO 3166-1 alpha-2), ProductCategory (CREDIT, DEBIT,
	// PREPAID) et IssuerName caractérisent la carte côté émetteur.
	// Cross-provider : Stripe les expose sous country / funding /
	// issuer. Vides quand l'enrôlement ne les a pas fournis.
	Country         string
	ProductCategory string
	IssuerName      string

	// Revoked : marqué true par un appel Revoke, empêche tout rejeu
	// avec ce token. Idempotent — un revoke sur token déjà révoqué
	// laisse simplement l'état true.
	Revoked bool

	// MetadataJSON : map[string]string libre du marchand.
	MetadataJSON string

	// ProviderDataJSON : champs spécifiques provider (auth 3DS
	// initiale, network transaction ID, ...).
	ProviderDataJSON string

	// CreatedAt : moment d'enregistrement du moyen. Pas d'UpdatedAt
	// distinct — un moyen de paiement enregistré ne se modifie pas,
	// il se révoque et un nouveau prend le relais.
	CreatedAt time.Time
}

// PaymentMethodRepository est le contrat de persistance des moyens de
// paiement enregistrés. Symétrique à PaymentRepository et
// SubscriptionRepository.
type PaymentMethodRepository interface {
	// Save insère ou met à jour. Idempotent sur même Token.
	Save(rec *PaymentMethodRecord) error

	// ByToken retourne l'entrée identifiée, ou nil, nil si inconnue.
	ByToken(token string) (*PaymentMethodRecord, error)

	// ByProvider retourne tous les moyens d'un provider, ordonnés par
	// CreatedAt décroissant.
	ByProvider(provider string) ([]*PaymentMethodRecord, error)

	// Revoke marque le moyen comme révoqué. Idempotent : token
	// inconnu ne remonte pas d'erreur (l'état demandé est atteint —
	// « ce token n'est plus utilisable » est vrai pour un token
	// inexistant).
	Revoke(token string) error

	// Count retourne le nombre total. Cross-provider.
	Count() (int, error)

	// Close libère les ressources sous-jacentes.
	Close() error
}
