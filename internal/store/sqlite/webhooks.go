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
			payment_uuid TEXT NOT NULL DEFAULT '',
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
	// Bases antérieures à ces colonnes : on tente l'ALTER et on ignore
	// le "duplicate column", qui signale que l'état voulu est déjà
	// atteint. Les livraisons déjà historisées gardent une valeur vide
	// — ni l'outcome ni le paiement ne se reconstituent sans relire
	// chaque body, et le second n'y figure même pas toujours.
	alters := []string{
		`ALTER TABLE webhooks ADD COLUMN outcome TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE webhooks ADD COLUMN payment_uuid TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range alters {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			if !isDuplicateColumnErr(err) {
				return fmt.Errorf("stmt %q: %w", firstLine(stmt), err)
			}
		}
	}
	// Index créé après l'ALTER : sur une base ancienne, la colonne
	// n'existe pas encore au moment où le bloc stmts s'exécute.
	if _, err := r.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_payment_uuid
			ON webhooks(payment_uuid, completed_at DESC)`); err != nil {
		return fmt.Errorf("index payment_uuid: %w", err)
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
			id, url, headers_json, body, status, outcome, payment_uuid,
			status_code, error_msg, attempts, created_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			headers_json = excluded.headers_json,
			body = excluded.body,
			status = excluded.status,
			outcome = excluded.outcome,
			payment_uuid = excluded.payment_uuid,
			status_code = excluded.status_code,
			error_msg = excluded.error_msg,
			attempts = excluded.attempts,
			completed_at = excluded.completed_at
	`
	_, err := r.db.Exec(upsert,
		rec.ID, rec.URL, nonEmpty(rec.HeadersJSON), rec.Body,
		rec.Status, rec.Outcome, rec.PaymentUUID,
		rec.StatusCode, rec.ErrorMsg, rec.Attempts,
		rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		rec.CompletedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// webhookColumns fixe l'ordre de lecture, que scanWebhook suit à la
// lettre. Les trois requêtes de lecture le partagent : une colonne
// ajoutée ici sans l'être dans scanWebhook casse les trois d'un coup,
// ce qui vaut mieux qu'une seule silencieusement décalée.
const webhookColumns = `id, url, headers_json, body, status, outcome, payment_uuid,
	status_code, error_msg, attempts, created_at, completed_at`

// Recent retourne les `limit` dernières entrées, plus récente d'abord.
func (r *WebhooksRepository) Recent(limit int) ([]*store.WebhookRecord, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT `+webhookColumns+`
		FROM webhooks
		ORDER BY completed_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	return collectWebhooks(rows)
}

// ByPayment retourne les livraisons d'un paiement, plus récente
// d'abord. Un uuid vide ne retourne rien plutôt que tout : les
// webhooks sans paiement rattaché ne forment pas un ensemble qu'on
// voudrait consulter, et le contraire ferait passer un filtre absent
// pour un filtre satisfait.
func (r *WebhooksRepository) ByPayment(paymentUUID string, limit int) ([]*store.WebhookRecord, error) {
	if paymentUUID == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT `+webhookColumns+`
		FROM webhooks
		WHERE payment_uuid = ?
		ORDER BY completed_at DESC
		LIMIT ?
	`, paymentUUID, limit)
	if err != nil {
		return nil, err
	}
	return collectWebhooks(rows)
}

// CountsByPayment compte les livraisons par paiement, en une requête.
//
// L'agrégation est faite par la base plutôt qu'en rapatriant les
// enregistrements : la liste des paiements a besoin du seul nombre, et
// l'historique persisté peut compter des milliers d'entrées.
func (r *WebhooksRepository) CountsByPayment() (map[string]int, error) {
	rows, err := r.db.Query(`
		SELECT payment_uuid, COUNT(*)
		FROM webhooks
		WHERE payment_uuid <> ''
		GROUP BY payment_uuid
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int)
	for rows.Next() {
		var uuid string
		var n int
		if err := rows.Scan(&uuid, &n); err != nil {
			return nil, err
		}
		counts[uuid] = n
	}
	return counts, rows.Err()
}

// collectWebhooks matérialise un curseur en enregistrements.
//
// Extrait des trois lectures qui parcourent les mêmes colonnes : sans
// cela, une requête ajoutée oublie facilement la fermeture du curseur ou
// le rows.Err() final, qui seul distingue une liste vide d'une lecture
// interrompue.
func collectWebhooks(rows *sql.Rows) ([]*store.WebhookRecord, error) {
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
		SELECT `+webhookColumns+`
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
		&rec.Status, &rec.Outcome, &rec.PaymentUUID,
		&rec.StatusCode, &rec.ErrorMsg, &rec.Attempts,
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
