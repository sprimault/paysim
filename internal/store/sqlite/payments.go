// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
	"github.com/sprimault/paysim/internal/store"
)

// PaymentsRepository est l'implémentation SQLite de
// store.PaymentRepository. Elle expose la table générique `payments`
// et sa table liée `payment_events` — schéma unique quel que soit le
// provider consommateur.
type PaymentsRepository struct {
	db *DB
}

// NewPaymentsRepository construit un PaymentsRepository et applique
// le schéma. Idempotent (CREATE TABLE IF NOT EXISTS) — ré-appliquable
// à chaque boot.
func NewPaymentsRepository(db *DB) (*PaymentsRepository, error) {
	r := &PaymentsRepository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("payments migrate: %w", err)
	}
	return r, nil
}

func (r *PaymentsRepository) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS payments (
			uuid TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			provider_ref TEXT NOT NULL,
			order_id TEXT NOT NULL,
			amount INTEGER NOT NULL,
			currency TEXT NOT NULL,
			state TEXT NOT NULL,
			refunded INTEGER NOT NULL DEFAULT 0,
			customer_json TEXT NOT NULL DEFAULT '{}',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			provider_data_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_provider_ref
			ON payments(provider, provider_ref)`,
		`CREATE INDEX IF NOT EXISTS idx_payments_updated_at
			ON payments(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_payments_order_id
			ON payments(order_id)`,
		`CREATE TABLE IF NOT EXISTS payment_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			payment_uuid TEXT NOT NULL,
			seq INTEGER NOT NULL,
			at TEXT NOT NULL,
			kind TEXT NOT NULL,
			amount INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (payment_uuid) REFERENCES payments(uuid) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_events_payment
			ON payment_events(payment_uuid, seq)`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("stmt %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// Save insère ou met à jour un PaymentRecord et ses events, dans une
// unique transaction SQL — l'atomicité garantit qu'un plantage entre
// l'upsert et l'écriture des events ne laisse pas d'état incohérent.
func (r *PaymentsRepository) Save(rec *store.PaymentRecord) error {
	if rec == nil {
		return errors.New("Save(nil)")
	}
	if rec.UUID == "" {
		return errors.New("Save: UUID vide")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		const upsert = `
			INSERT INTO payments (
				uuid, provider, provider_ref, order_id, amount, currency,
				state, refunded,
				customer_json, metadata_json, provider_data_json,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(uuid) DO UPDATE SET
				provider = excluded.provider,
				provider_ref = excluded.provider_ref,
				order_id = excluded.order_id,
				amount = excluded.amount,
				currency = excluded.currency,
				state = excluded.state,
				refunded = excluded.refunded,
				customer_json = excluded.customer_json,
				metadata_json = excluded.metadata_json,
				provider_data_json = excluded.provider_data_json,
				updated_at = excluded.updated_at
		`
		_, err := tx.ExecContext(ctx, upsert,
			rec.UUID, rec.Provider, rec.ProviderRef, rec.OrderID,
			int64(rec.Amount), rec.Currency, string(rec.State), int64(rec.Refunded),
			nonEmpty(rec.CustomerJSON), nonEmpty(rec.MetadataJSON), nonEmpty(rec.ProviderDataJSON),
			rec.CreatedAt.UTC().Format(time.RFC3339Nano),
			rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
		// Events réécrits de zéro. Simple, sans perte : les events
		// sont append-only côté domain, un Save avec plus d'events
		// remplace juste l'ensemble avec le même contenu prefixé.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM payment_events WHERE payment_uuid = ?`, rec.UUID); err != nil {
			return err
		}
		const insertEvent = `
			INSERT INTO payment_events (payment_uuid, seq, at, kind, amount, note)
			VALUES (?, ?, ?, ?, ?, ?)
		`
		for i, e := range rec.Events {
			if _, err := tx.ExecContext(ctx, insertEvent,
				rec.UUID, i, e.At.UTC().Format(time.RFC3339Nano),
				string(e.Kind), int64(e.Amount), e.Note,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// ByUUID récupère un paiement + ses events via son UUID.
func (r *PaymentsRepository) ByUUID(uuid string) (*store.PaymentRecord, error) {
	if uuid == "" {
		return nil, nil
	}
	rec, err := r.scanOneWhere("uuid = ?", uuid)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	events, err := r.loadEvents(rec.UUID)
	if err != nil {
		return nil, err
	}
	rec.Events = events
	return rec, nil
}

// ByProviderRef récupère un paiement via son couple (provider,
// providerRef). L'index unique idx_payments_provider_ref garantit
// l'unicité.
func (r *PaymentsRepository) ByProviderRef(provider, providerRef string) (*store.PaymentRecord, error) {
	if providerRef == "" {
		return nil, nil
	}
	rec, err := r.scanOneWhere("provider = ? AND provider_ref = ?", provider, providerRef)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	events, err := r.loadEvents(rec.UUID)
	if err != nil {
		return nil, err
	}
	rec.Events = events
	return rec, nil
}

// All retourne tous les paiements, plus récent d'abord.
func (r *PaymentsRepository) All() ([]*store.PaymentRecord, error) {
	return r.scanMany("", nil)
}

// ByProvider filtre All sur un provider spécifique.
func (r *PaymentsRepository) ByProvider(provider string) ([]*store.PaymentRecord, error) {
	return r.scanMany("WHERE provider = ?", []any{provider})
}

// Count retourne le nombre total (cross-provider).
func (r *PaymentsRepository) Count() (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&n)
	return n, err
}

// DeleteByUUID supprime un paiement — les payment_events tombent par
// ON DELETE CASCADE. Idempotent : un UUID inconnu ne remonte pas
// d'erreur.
func (r *PaymentsRepository) DeleteByUUID(uuid string) error {
	if uuid == "" {
		return nil
	}
	_, err := r.db.Exec(`DELETE FROM payments WHERE uuid = ?`, uuid)
	return err
}

// DeleteByProvider supprime tous les paiements d'un provider. Retourne
// le nombre effectivement supprimé.
func (r *PaymentsRepository) DeleteByProvider(provider string) (int, error) {
	if provider == "" {
		return 0, nil
	}
	res, err := r.db.Exec(`DELETE FROM payments WHERE provider = ?`, provider)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// DeleteAll purge la table. Utile pour un reset côté UI (« vider les
// paiements ») ou pour un dev qui veut repartir de zéro sans redémarrer.
func (r *PaymentsRepository) DeleteAll() (int, error) {
	res, err := r.db.Exec(`DELETE FROM payments`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// Close ferme la DB sous-jacente.
func (r *PaymentsRepository) Close() error {
	return r.db.Close()
}

// -----------------------------------------------------------------------------
// Helpers privés
// -----------------------------------------------------------------------------

// scanOneWhere lit une ligne unique en filtrant sur une clause WHERE.
// La clause est en dur côté callers (pas d'input utilisateur), pas de
// risque d'injection.
func (r *PaymentsRepository) scanOneWhere(whereClause string, args ...any) (*store.PaymentRecord, error) {
	// #nosec G202 -- whereClause vient de constantes internes.
	query := `
		SELECT uuid, provider, provider_ref, order_id, amount, currency,
			state, refunded,
			customer_json, metadata_json, provider_data_json,
			created_at, updated_at
		FROM payments WHERE ` + whereClause
	row := r.db.QueryRow(query, args...)
	return scanPayment(row)
}

// scanMany exécute une query multi-lignes et charge chaque paiement +
// ses events. N+1 assumé — le simulateur reste sur des volumes faibles.
func (r *PaymentsRepository) scanMany(whereClause string, args []any) ([]*store.PaymentRecord, error) {
	query := `
		SELECT uuid, provider, provider_ref, order_id, amount, currency,
			state, refunded,
			customer_json, metadata_json, provider_data_json,
			created_at, updated_at
		FROM payments `
	if whereClause != "" {
		query += whereClause + " "
	}
	query += `ORDER BY updated_at DESC`
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*store.PaymentRecord
	for rows.Next() {
		rec, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Charger les events pour chaque paiement.
	for _, rec := range out {
		events, err := r.loadEvents(rec.UUID)
		if err != nil {
			return nil, err
		}
		rec.Events = events
	}
	return out, nil
}

// loadEvents charge les events d'un paiement dans l'ordre seq.
func (r *PaymentsRepository) loadEvents(uuid string) ([]domain.Event, error) {
	rows, err := r.db.Query(
		`SELECT at, kind, amount, note FROM payment_events
		 WHERE payment_uuid = ? ORDER BY seq`, uuid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Event
	for rows.Next() {
		var atStr, kind, note string
		var amt int64
		if err := rows.Scan(&atStr, &kind, &amt, &note); err != nil {
			return nil, err
		}
		at, err := time.Parse(time.RFC3339Nano, atStr)
		if err != nil {
			return nil, fmt.Errorf("parse event.at %q: %w", atStr, err)
		}
		out = append(out, domain.Event{
			At:     at,
			Kind:   domain.EventKind(kind),
			Amount: format.Amount(amt),
			Note:   note,
		})
	}
	return out, rows.Err()
}

// scanPayment lit une ligne de payments dans un PaymentRecord sans
// ses events (chargés séparément par l'appelant).
type paymentScanner interface {
	Scan(dest ...any) error
}

func scanPayment(sc paymentScanner) (*store.PaymentRecord, error) {
	var rec store.PaymentRecord
	var amount, refunded int64
	var stateStr, createdAtStr, updatedAtStr string
	err := sc.Scan(
		&rec.UUID, &rec.Provider, &rec.ProviderRef, &rec.OrderID,
		&amount, &rec.Currency, &stateStr, &refunded,
		&rec.CustomerJSON, &rec.MetadataJSON, &rec.ProviderDataJSON,
		&createdAtStr, &updatedAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Amount = format.Amount(amount)
	rec.Refunded = format.Amount(refunded)
	rec.State = domain.State(stateStr)
	rec.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	rec.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &rec, nil
}

// nonEmpty retourne "{}" si s est vide — évite les NULL et laisse
// json.Unmarshal recevoir un objet vide valide.
func nonEmpty(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

// firstLine extrait la première ligne d'un statement SQL pour un
// message d'erreur lisible.
func firstLine(stmt string) string {
	for i, c := range stmt {
		if c == '\n' {
			return stmt[:i]
		}
	}
	return stmt
}
