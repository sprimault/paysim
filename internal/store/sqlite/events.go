// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

// EventsRepository est l'impl SQLite de store.EventRepository.
type EventsRepository struct {
	db *DB
}

// NewEventsRepository construit le repository et applique le schéma.
func NewEventsRepository(db *DB) (*EventsRepository, error) {
	r := &EventsRepository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("events migrate: %w", err)
	}
	return r, nil
}

func (r *EventsRepository) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS bus_events (
			id INTEGER PRIMARY KEY,
			type TEXT NOT NULL,
			at TEXT NOT NULL,
			data_json TEXT NOT NULL DEFAULT '{}'
		)`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("stmt %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// Save insère un event (ID monotone assigné par le bus).
func (r *EventsRepository) Save(rec store.EventRecord) error {
	if rec.ID == 0 {
		return errors.New("Save: ID zero (attendu monotone > 0)")
	}
	_, err := r.db.Exec(
		`INSERT INTO bus_events (id, type, at, data_json)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		rec.ID, rec.Type, rec.At.UTC().Format(time.RFC3339Nano),
		nonEmpty(rec.DataJSON),
	)
	return err
}

// Since retourne les events avec ID > lastID, ordonnés croissants.
func (r *EventsRepository) Since(lastID uint64) ([]store.EventRecord, error) {
	rows, err := r.db.Query(
		`SELECT id, type, at, data_json FROM bus_events
		 WHERE id > ? ORDER BY id`, lastID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []store.EventRecord
	for rows.Next() {
		var rec store.EventRecord
		var atStr string
		if err := rows.Scan(&rec.ID, &rec.Type, &atStr, &rec.DataJSON); err != nil {
			return nil, err
		}
		rec.At, err = time.Parse(time.RFC3339Nano, atStr)
		if err != nil {
			return nil, fmt.Errorf("parse at: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteBefore supprime les events plus anciens que id (inclus).
func (r *EventsRepository) DeleteBefore(id uint64) (int, error) {
	res, err := r.db.Exec(`DELETE FROM bus_events WHERE id <= ?`, id)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// Close ferme la DB sous-jacente.
func (r *EventsRepository) Close() error {
	return r.db.Close()
}
