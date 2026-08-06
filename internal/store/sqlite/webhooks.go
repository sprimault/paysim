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

// WebhooksRepository est l'impl SQLite de store.WebhookRepository.
type WebhooksRepository struct {
	db *DB
}

// NewWebhooksRepository construit un WebhooksRepository et applique
// le schéma. Idempotent — ré-appliquable à chaque boot.
func NewWebhooksRepository(db *DB) (*WebhooksRepository, error) {
	r := &WebhooksRepository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("webhooks migrate: %w", err)
	}
	return r, nil
}

func (r *WebhooksRepository) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS webhooks (
			id TEXT PRIMARY KEY,
			url TEXT NOT NULL,
			headers_json TEXT NOT NULL DEFAULT '{}',
			body BLOB NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			outcome TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 0,
			error_msg TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			completed_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_completed_at
			ON webhooks(completed_at DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("stmt %q: %w", firstLine(stmt), err)
		}
	}
	// Bases antérieures à l'ajout de l'outcome : on tente l'ALTER et on
	// ignore le "duplicate column", qui signale que l'état voulu est
	// déjà atteint. Les livraisons déjà historisées gardent un outcome
	// vide — on ne peut pas le reconstituer sans relire chaque body.
	if _, err := r.db.ExecContext(ctx,
		`ALTER TABLE webhooks ADD COLUMN outcome TEXT NOT NULL DEFAULT ''`); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf("add outcome column: %w", err)
		}
	}
	return nil
}

// Save insère ou remplace un WebhookRecord.
func (r *WebhooksRepository) Save(rec *store.WebhookRecord) error {
	if rec == nil {
		return errors.New("Save(nil)")
	}
	if rec.ID == "" {
		return errors.New("Save: ID vide")
	}
	const upsert = `
		INSERT INTO webhooks (
			id, url, headers_json, body, status, outcome, status_code, error_msg,
			attempts, created_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			headers_json = excluded.headers_json,
			body = excluded.body,
			status = excluded.status,
			outcome = excluded.outcome,
			status_code = excluded.status_code,
			error_msg = excluded.error_msg,
			attempts = excluded.attempts,
			completed_at = excluded.completed_at
	`
	_, err := r.db.Exec(upsert,
		rec.ID, rec.URL, nonEmpty(rec.HeadersJSON), rec.Body,
		rec.Status, rec.Outcome, rec.StatusCode, rec.ErrorMsg, rec.Attempts,
		rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		rec.CompletedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// Recent retourne les `limit` dernières entrées, plus récente d'abord.
func (r *WebhooksRepository) Recent(limit int) ([]*store.WebhookRecord, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT id, url, headers_json, body, status, outcome, status_code, error_msg,
			attempts, created_at, completed_at
		FROM webhooks
		ORDER BY completed_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*store.WebhookRecord
	for rows.Next() {
		rec, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ByID récupère un webhook par son ID.
func (r *WebhooksRepository) ByID(id string) (*store.WebhookRecord, error) {
	if id == "" {
		return nil, nil
	}
	row := r.db.QueryRow(`
		SELECT id, url, headers_json, body, status, outcome, status_code, error_msg,
			attempts, created_at, completed_at
		FROM webhooks WHERE id = ?
	`, id)
	return scanWebhook(row)
}

// DeleteAll purge la table.
func (r *WebhooksRepository) DeleteAll() (int, error) {
	res, err := r.db.Exec(`DELETE FROM webhooks`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// Close ferme la DB sous-jacente.
func (r *WebhooksRepository) Close() error {
	return r.db.Close()
}

func scanWebhook(sc paymentScanner) (*store.WebhookRecord, error) {
	var rec store.WebhookRecord
	var createdAtStr, completedAtStr string
	err := sc.Scan(
		&rec.ID, &rec.URL, &rec.HeadersJSON, &rec.Body,
		&rec.Status, &rec.Outcome, &rec.StatusCode, &rec.ErrorMsg, &rec.Attempts,
		&createdAtStr, &completedAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	rec.CompletedAt, err = time.Parse(time.RFC3339Nano, completedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse completed_at: %w", err)
	}
	return &rec, nil
}
