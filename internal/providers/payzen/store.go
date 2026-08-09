// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

// Store est le contrat de persistance des transactions et abonnements
// PayZen. Deux implémentations sont fournies dans ce paquet :
//
//   - RepoStore (NewRepoStore) : wrapper sur les trois repositories
//     cross-provider de internal/store. Ne dépend d'aucun backend —
//     c'est lui qu'emploie cmd/paysim dans les deux modes, adossé à
//     internal/store/sqlite ou à internal/store/inmem selon
//     PAYSIM_STORE.
//
// Une seule implémentation, volontairement. Il en existait une seconde,
// MemoryStore, qui servait au mode mémoire : deux traductions du même
// contrat, dont une que la production a cessé d'emprunter sans que les
// tests le remarquent. C'est ce qui a permis au mode par défaut de
// répondre « liste vide » sur des objets existants jusqu'en v0.6.1.
//
// Le contrat est testé dans store_contract_test.go, joué sur les deux
// backends de RepoStore — un ajout de méthode ici doit être suivi d'un
// test contract associé.
//
// Toute erreur de persistance (SQLite plein, corruption) remonte via
// error. Les dépôts en mémoire ne peuvent pas échouer et retournent
// toujours nil ; la signature est identique pour ne pas diverger.
type Store interface {
	// Save indexe une transaction sous FormToken + UUID. Écrase
	// silencieusement une transaction existante — mise à jour
	// d'état légitime.
	Save(tx *Transaction) error

	// ByToken retourne la transaction associée à un formToken, ou
	// nil, nil si inconnue.
	ByToken(token string) (*Transaction, error)

	// ByUUID retourne la transaction associée à un UUID, ou nil, nil
	// si inconnue.
	ByUUID(uuid string) (*Transaction, error)

	// Len retourne le nombre de transactions distinctes indexées.
	// Utile pour l'observabilité et les tests.
	Len() (int, error)

	// AllTransactions retourne un snapshot de toutes les transactions
	// indexées. Ordre non garanti (map iteration pour la mémoire,
	// ORDER BY updated_at DESC pour SQLite).
	AllTransactions() ([]*Transaction, error)

	// SaveSubscription indexe un abonnement par son ID. Écrase
	// silencieusement une entrée existante.
	SaveSubscription(sub *Subscription) error

	// SubscriptionByID retourne l'abonnement associé à l'ID, ou nil,
	// nil si inconnu.
	SubscriptionByID(id string) (*Subscription, error)

	// LenSubscriptions retourne le nombre d'abonnements indexés.
	LenSubscriptions() (int, error)

	// Delete supprime une transaction identifiée par son UUID.
	// Idempotent : un UUID inconnu ne remonte pas d'erreur.
	Delete(uuid string) error

	// DeleteAllTransactions supprime toutes les transactions PayZen
	// (pas les abonnements). Retourne le nombre supprimé.
	DeleteAllTransactions() (int, error)

	// SaveMethod indexe un moyen de paiement enregistré par son Token.
	// Écrase silencieusement une entrée existante — utile pour marquer
	// un revoke ou une mise à jour de contexte.
	SaveMethod(m *PaymentMethod) error

	// MethodByToken retourne le PaymentMethod indexé, ou nil, nil si
	// inconnu.
	MethodByToken(token string) (*PaymentMethod, error)

	// RevokeMethod marque le PaymentMethod comme révoqué. Idempotent :
	// un token inconnu ne remonte pas d'erreur (l'état demandé « ce
	// token n'est plus utilisable » est atteint).
	RevokeMethod(token string) error

	// Close libère les ressources sous-jacentes — utile adossé à
	// SQLite, no-op en mémoire. L'appelant doit toujours l'appeler à
	// l'arrêt propre.
	Close() error
}
