// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sprimault/paysim/internal/format"
	"github.com/sprimault/paysim/internal/store"
)

// SubscriptionsRepository est l'implémentation SQLite de
// store.SubscriptionRepository. Table unique `subscriptions` partagée
// entre tous les providers ; les colonnes typées permettent des
// requêtes cross-provider directes.
type SubscriptionsRepository struct {
	db *DB
}

// NewSubscriptionsRepository construit le repo et applique la
// migration. Idempotent (CREATE TABLE IF NOT EXISTS).
func NewSubscriptionsRepository(db *DB) (*SubscriptionsRepository, error) {
	r := &SubscriptionsRepository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("subscriptions migrate: %w", err)
	}
	return r, nil
}

func (r *SubscriptionsRepository) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			order_id TEXT NOT NULL DEFAULT '',
			amount INTEGER NOT NULL,
			currency TEXT NOT NULL,
			payment_method_token TEXT NOT NULL,
			effect_date TEXT NOT NULL DEFAULT '',
			rrule TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			provider_data_json TEXT NOT NULL DEFAULT '{}',
			cancelled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscriptions_provider
			ON subscriptions(provider, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_subscriptions_method_token
			ON subscriptions(payment_method_token)`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("stmt %q: %w", firstLine(stmt), err)
		}
	}
	// ALTER TABLE ADD COLUMN pour les bases préexistantes (5a livré
	// sans cette colonne). SQLite renvoie "duplicate column" si elle
	// existe déjà — on ignore silencieusement, l'état demandé est
	// atteint. Approche standard SQLite pour migration incrémentale.
	if _, err := r.db.ExecContext(ctx,
		`ALTER TABLE subscriptions ADD COLUMN cancelled INTEGER NOT NULL DEFAULT 0`); err != nil {
		// Ignorer "duplicate column" ; propager toute autre erreur.
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf("add cancelled column: %w", err)
		}
	}
	return nil
}

// isDuplicateColumnErr identifie l'erreur SQLite renvoyée par
// ALTER TABLE ADD COLUMN quand la colonne existe déjà. modernc.org/sqlite
// expose le message brut — pas de code sentinelle typé — on matche sur
// la sous-chaîne stable de SQLite.
func isDuplicateColumnErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}

// Save insère ou met à jour un SubscriptionRecord.
func (r *SubscriptionsRepository) Save(rec *store.SubscriptionRecord) error {
	if rec == nil {
		return errors.New("Save(nil)")
	}
	if rec.ID == "" {
		return errors.New("Save: ID vide")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const upsert = `
		INSERT INTO subscriptions (
			id, provider, order_id, amount, currency,
			payment_method_token, effect_date, rrule,
			metadata_json, provider_data_json, cancelled,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider = excluded.provider,
			order_id = excluded.order_id,
			amount = excluded.amount,
			currency = excluded.currency,
			payment_method_token = excluded.payment_method_token,
			effect_date = excluded.effect_date,
			rrule = excluded.rrule,
			metadata_json = excluded.metadata_json,
			provider_data_json = excluded.provider_data_json,
			cancelled = excluded.cancelled,
			updated_at = excluded.updated_at`
	_, err := r.db.ExecContext(ctx, upsert,
		rec.ID, rec.Provider, rec.OrderID, int64(rec.Amount), rec.Currency,
		rec.PaymentMethodToken, rec.EffectDate, rec.Rrule,
		rec.MetadataJSON, rec.ProviderDataJSON, boolToInt(rec.Cancelled),
		rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("subscriptions upsert %s: %w", rec.ID, err)
	}
	return nil
}

// ByID retourne l'abonnement identifié ou nil, nil si inconnu.
func (r *SubscriptionsRepository) ByID(id string) (*store.SubscriptionRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const q = `SELECT id, provider, order_id, amount, currency,
		payment_method_token, effect_date, rrule,
		metadata_json, provider_data_json, cancelled, created_at, updated_at
		FROM subscriptions WHERE id = ?`
	row := r.db.QueryRowContext(ctx, q, id)
	rec, err := scanSubscription(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return rec, err
}

// ByProvider retourne tous les abonnements d'un provider, ordonnés par
// updated_at décroissant.
func (r *SubscriptionsRepository) ByProvider(provider string) ([]*store.SubscriptionRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const q = `SELECT id, provider, order_id, amount, currency,
		payment_method_token, effect_date, rrule,
		metadata_json, provider_data_json, cancelled, created_at, updated_at
		FROM subscriptions WHERE provider = ? ORDER BY updated_at DESC`
	rows, err := r.db.QueryContext(ctx, q, provider)
	if err != nil {
		return nil, fmt.Errorf("subscriptions ByProvider: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*store.SubscriptionRecord
	for rows.Next() {
		rec, err := scanSubscription(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Count retourne le nombre total (cross-provider).
func (r *SubscriptionsRepository) Count() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions`).Scan(&n)
	return n, err
}

// DeleteByID supprime silencieusement — idempotent.
func (r *SubscriptionsRepository) DeleteByID(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = ?`, id)
	return err
}

// DeleteByProvider retourne le nombre effectivement supprimé.
func (r *SubscriptionsRepository) DeleteByProvider(provider string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := r.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE provider = ?`, provider)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Close no-op — le DB sous-jacent est possédé par l'appelant.
func (r *SubscriptionsRepository) Close() error { return nil }

// Cancel marque l'abonnement comme annulé. Idempotent (ID inconnu
// → no-op sans erreur, cf. contrat).
func (r *SubscriptionsRepository) Cancel(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.db.ExecContext(ctx,
		`UPDATE subscriptions SET cancelled = 1 WHERE id = ?`, id)
	return err
}

// scanSubscription est le lecteur commun à ByID et ByProvider,
// factorisé pour éviter la duplication de l'ordre des colonnes.
func scanSubscription(scan func(dest ...any) error) (*store.SubscriptionRecord, error) {
	var (
		rec                  store.SubscriptionRecord
		amount               int64
		cancelled            int
		createdAt, updatedAt string
	)
	if err := scan(
		&rec.ID, &rec.Provider, &rec.OrderID, &amount, &rec.Currency,
		&rec.PaymentMethodToken, &rec.EffectDate, &rec.Rrule,
		&rec.MetadataJSON, &rec.ProviderDataJSON, &cancelled,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	rec.Amount = format.Amount(amount)
	rec.Cancelled = cancelled != 0
	ca, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	ua, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	rec.CreatedAt = ca
	rec.UpdatedAt = ua
	return &rec, nil
}
