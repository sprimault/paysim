// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

// Store est le contrat de persistance des transactions et abonnements
// PayZen. Deux implémentations sont fournies dans ce paquet :
//
//   - MemoryStore (NewMemoryStore) : maps protégées par mutex, sans état
//     entre redémarrages. Par défaut si aucune persistance n'est configurée.
//   - SQLiteStore (NewSQLiteStore) : persistance sur disque via
//     internal/store/sqlite. Activée quand PAYSIM_STORE=sqlite.
//
// Les deux impls partagent le même contrat testé dans
// store_contract_test.go — un ajout de méthode ici doit être suivi
// d'un ajout dans les deux impls et d'un test contract associé.
//
// Toute erreur de persistance (SQLite plein, corruption) remonte via
// error. MemoryStore ne peut pas échouer et retourne toujours nil ;
// la signature est identique pour ne pas divergér.
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

	// Close libère les ressources (utile pour SQLiteStore ; no-op
	// pour MemoryStore). L'appelant doit toujours l'appeler à
	// l'arrêt propre.
	Close() error
}
