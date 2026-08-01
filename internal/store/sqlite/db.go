// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package sqlite fournit un wrapper minimaliste autour de database/sql
// utilisant le driver pure Go modernc.org/sqlite. Aucun schéma métier
// ici — chaque paquet consommateur (payzen, delivery, bus) définit
// ses tables et ses requêtes dans son propre fichier _sqlite.go.
//
// Le rôle de ce paquet :
//   - Ouvrir le fichier SQLite avec les pragmas standards Paysim (WAL,
//     foreign keys, busy timeout, synchronous NORMAL).
//   - Fournir un helper de transaction avec rollback automatique en
//     cas d'erreur.
//   - Sérialiser les écritures via MaxOpenConns=1 — SQLite gère mal
//     l'écriture concurrente sinon (SQLITE_BUSY random). Les lectures
//     restent concurrentes grâce au mode WAL.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // driver pure Go, s'enregistre au sql.Register
)

// driverName est le nom sous lequel modernc.org/sqlite s'enregistre.
// (Note : diffère de mattn/go-sqlite3 qui utilise "sqlite3".)
const driverName = "sqlite"

// DB embarque *sql.DB pour exposer l'API standard database/sql tout
// en ajoutant les helpers InTx / Close qui composent bien avec les
// consommateurs.
type DB struct {
	*sql.DB
}

// Open ouvre une connexion SQLite au chemin donné, applique les
// pragmas standards Paysim, et vérifie la connexion (Open lui-même
// est paresseux). Le fichier est créé s'il n'existe pas.
//
// Pragmas appliqués :
//   - journal_mode=WAL   : lectures concurrentes durant les writes ;
//     durabilité au niveau standard SQLite.
//   - foreign_keys=ON    : SQLite les désactive par défaut, on force
//     l'enforcement pour attraper les violations tôt.
//   - busy_timeout=5000  : attend jusqu'à 5s si un writer tient la
//     base — évite SQLITE_BUSY immédiat sur contention légère.
//   - synchronous=NORMAL : compromis raisonnable en mode WAL (FULL
//     trop lent, OFF ne garantit rien à un crash).
func Open(path string) (*DB, error) {
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("sqlite open %s: %w", path, err)
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply %s: %w", p, err)
		}
	}
	// Ping pour valider la connexion (Open est paresseux).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	// SQLite gère mal l'écriture concurrente : un seul writer à la
	// fois, sérialisé au niveau du pool. Les lectures WAL restent
	// concurrentes (pas d'impact perf sur les SELECT).
	db.SetMaxOpenConns(1)
	return &DB{DB: db}, nil
}

// InTx exécute fn dans une transaction. Rollback automatique en cas
// d'erreur retournée par fn, commit sinon. La transaction est
// abandonnée si le contexte est annulé.
//
// Utile pour les CRUD multi-statements qui doivent être atomiques —
// exemple typique : sauvegarder un paiement et enregistrer un
// événement en une seule opération.
func (d *DB) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
