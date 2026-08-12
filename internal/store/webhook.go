// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package store

import "time"

// WebhookRecord est la représentation persistante d'une tentative de
// livraison de webhook, commune à tous les providers. Les webhooks
// ne sont pas provider-spécifiques par nature (POST HTTP avec Body
// et Headers) — pas besoin des blobs JSON provider comme pour les
// payments.
//
// Body est stocké tel quel (BLOB SQLite) — les payloads PayZen sont
// des form-urlencoded ASCII, les payloads Stripe seront du JSON UTF-8,
// tous restent des séquences d'octets neutres. HeadersJSON sérialise
// la map[string]string des headers HTTP.
type WebhookRecord struct {
	// ID identifie de manière unique une tentative de livraison. Un
	// rejeu génère un nouveau ID pour tracer chaque tentative
	// distinctement.
	ID string

	// URL cible du POST.
	URL string

	// HeadersJSON sérialise map[string]string des headers HTTP posés
	// sur la requête. Format : {"Content-Type": "…", "X-...": "…"}.
	HeadersJSON string

	// Body est le corps HTTP envoyé, tel quel (bytes).
	Body []byte

	// Status : "delivered" | "failed" | "pending". Décrit l'acheminement
	// HTTP, pas le contenu.
	Status string

	// Outcome est le résultat métier annoncé par le webhook, dans le
	// vocabulaire du provider ("PAID", "UNPAID"…). Renseigné par
	// l'adaptateur à l'émission. Vide pour un webhook qui n'annonce pas
	// de résultat de paiement.
	Outcome string

	// PaymentUUID rattache la livraison au paiement qui l'a provoquée,
	// vide si elle n'en concerne aucun. C'est ce qui permet à l'UI de
	// n'afficher que les webhooks du paiement ouvert.
	PaymentUUID string

	// StatusCode est le code HTTP reçu. Vaut 0 si l'erreur s'est
	// produite avant la réception d'une réponse (timeout, DNS…).
	StatusCode int

	// ErrorMsg est le message d'erreur pour Status="failed",
	// vide sinon.
	ErrorMsg string

	// Attempts compte les tentatives — vaut 1 pour un premier envoi
	// réussi ou échoué, incrémente sur les rejeux.
	Attempts int

	// CreatedAt : entrée en file. CompletedAt : fin de tentative.
	CreatedAt   time.Time
	CompletedAt time.Time
}

// WebhookRepository est le contrat de persistance de l'historique des
// livraisons de webhooks. Toute impl (mémoire, SQLite) le respecte.
type WebhookRepository interface {
	// Save persiste (ou remplace si l'ID existe déjà) un enregistrement
	// complet — un rejeu est un nouvel ID.
	Save(rec *WebhookRecord) error

	// Recent retourne les `limit` dernières entrées, plus récente
	// d'abord (order by completed_at DESC).
	Recent(limit int) ([]*WebhookRecord, error)

	// ByID retourne un enregistrement par son ID, ou nil, nil si
	// inconnu.
	ByID(id string) (*WebhookRecord, error)

	// ByPayment retourne les `limit` dernières livraisons rattachées à
	// un paiement, plus récente d'abord. Interroger la base plutôt que
	// filtrer les N dernières entrées : sur un paiement un peu ancien,
	// ses webhooks sont sortis de la fenêtre de Recent alors qu'ils
	// existent toujours.
	ByPayment(paymentUUID string, limit int) ([]*WebhookRecord, error)

	// CountsByPayment compte les livraisons de chaque paiement, en une
	// lecture. La liste des paiements en affiche le nombre par ligne :
	// une requête par paiement ferait des centaines d'allers-retours
	// pour une page.
	//
	// Les entrées sans paiement rattaché sont hors décompte — elles
	// n'appartiennent à aucune ligne.
	CountsByPayment() (map[string]int, error)

	// DeleteAll purge l'historique. Retourne le nombre supprimé.
	DeleteAll() (int, error)

	// Close libère les ressources sous-jacentes (no-op pour l'impl
	// mémoire, checkpoint WAL pour SQLite).
	Close() error
}
