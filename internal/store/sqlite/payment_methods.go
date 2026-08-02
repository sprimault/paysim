// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

// PaymentMethodsRepository est l'implémentation SQLite de
// store.PaymentMethodRepository. Table unique `payment_methods`
// partagée entre tous les providers.
//
// AVERTISSEMENT : PAN complet stocké en clair (simulateur, aucune
// protection PCI-DSS). Voir la doc install et l'invariant projet.
type PaymentMethodsRepository struct {
	db *DB
}

// NewPaymentMethodsRepository construit le repo et applique la
// migration. Idempotent.
func NewPaymentMethodsRepository(db *DB) (*PaymentMethodsRepository, error) {
	r := &PaymentMethodsRepository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("payment_methods migrate: %w", err)
	}
	return r, nil
}

func (r *PaymentMethodsRepository) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS payment_methods (
			token TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			pan_full TEXT NOT NULL,
			pan_masked TEXT NOT NULL,
			brand TEXT NOT NULL DEFAULT '',
			expiry_month INTEGER NOT NULL,
			expiry_year INTEGER NOT NULL,
			revoked INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			provider_data_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_methods_provider
			ON payment_methods(provider, created_at DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("stmt %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// Save insère ou met à jour un PaymentMethodRecord.
func (r *PaymentMethodsRepository) Save(rec *store.PaymentMethodRecord) error {
	if rec == nil {
		return errors.New("Save(nil)")
	}
	if rec.Token == "" {
		return errors.New("Save: Token vide")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const upsert = `
		INSERT INTO payment_methods (
			token, provider, pan_full, pan_masked, brand,
			expiry_month, expiry_year, revoked,
			metadata_json, provider_data_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET
			provider = excluded.provider,
			pan_full = excluded.pan_full,
			pan_masked = excluded.pan_masked,
			brand = excluded.brand,
			expiry_month = excluded.expiry_month,
			expiry_year = excluded.expiry_year,
			revoked = excluded.revoked,
			metadata_json = excluded.metadata_json,
			provider_data_json = excluded.provider_data_json`
	_, err := r.db.ExecContext(ctx, upsert,
		rec.Token, rec.Provider, rec.PANFull, rec.PANMasked, rec.Brand,
		rec.ExpiryMonth, rec.ExpiryYear, boolToInt(rec.Revoked),
		rec.MetadataJSON, rec.ProviderDataJSON,
		rec.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("payment_methods upsert %s: %w", rec.Token, err)
	}
	return nil
}

// ByToken retourne le moyen identifié ou nil, nil si inconnu.
func (r *PaymentMethodsRepository) ByToken(token string) (*store.PaymentMethodRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const q = `SELECT token, provider, pan_full, pan_masked, brand,
		expiry_month, expiry_year, revoked,
		metadata_json, provider_data_json, created_at
		FROM payment_methods WHERE token = ?`
	row := r.db.QueryRowContext(ctx, q, token)
	rec, err := scanPaymentMethod(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return rec, err
}

// ByProvider retourne tous les moyens d'un provider, ordonnés par
// created_at décroissant.
func (r *PaymentMethodsRepository) ByProvider(provider string) ([]*store.PaymentMethodRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const q = `SELECT token, provider, pan_full, pan_masked, brand,
		expiry_month, expiry_year, revoked,
		metadata_json, provider_data_json, created_at
		FROM payment_methods WHERE provider = ? ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, q, provider)
	if err != nil {
		return nil, fmt.Errorf("payment_methods ByProvider: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*store.PaymentMethodRecord
	for rows.Next() {
		rec, err := scanPaymentMethod(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Revoke marque le moyen comme révoqué. Idempotent : token inconnu
// n'est pas une erreur (l'état demandé est atteint).
func (r *PaymentMethodsRepository) Revoke(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.db.ExecContext(ctx,
		`UPDATE payment_methods SET revoked = 1 WHERE token = ?`, token)
	return err
}

// Count total, cross-provider.
func (r *PaymentMethodsRepository) Count() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_methods`).Scan(&n)
	return n, err
}

// Close no-op — DB possédée par l'appelant.
func (r *PaymentMethodsRepository) Close() error { return nil }

// scanPaymentMethod lecteur commun à ByToken et ByProvider.
func scanPaymentMethod(scan func(dest ...any) error) (*store.PaymentMethodRecord, error) {
	var (
		rec       store.PaymentMethodRecord
		revoked   int
		createdAt string
	)
	if err := scan(
		&rec.Token, &rec.Provider, &rec.PANFull, &rec.PANMasked, &rec.Brand,
		&rec.ExpiryMonth, &rec.ExpiryYear, &revoked,
		&rec.MetadataJSON, &rec.ProviderDataJSON, &createdAt,
	); err != nil {
		return nil, err
	}
	rec.Revoked = revoked != 0
	ca, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	rec.CreatedAt = ca
	return &rec, nil
}

// boolToInt convertit bool→int pour SQLite (qui n'a pas de type booléen
// natif — INTEGER 0/1 est la convention idiomatique).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
