// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helperOpen ouvre une base SQLite temporaire dans le TempDir du test.
// Le fichier est automatiquement nettoyé à la fin du test par t.TempDir.
func helperOpen(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenCreatesFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sub", "test.db")
	// Le rep parent n'existe pas volontairement — Open doit échouer.
	_, err := Open(path)
	if err == nil {
		t.Fatal("Open sur rep inexistant doit échouer")
	}

	path = filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("fichier SQLite non créé: %v", err)
	}
}

func TestOpenAppliesPragmas(t *testing.T) {
	t.Parallel()
	db := helperOpen(t)

	cases := []struct {
		pragma, wantContains string
	}{
		{"PRAGMA journal_mode", "wal"},
		{"PRAGMA foreign_keys", "1"},
		{"PRAGMA busy_timeout", "5000"},
		{"PRAGMA synchronous", "1"}, // NORMAL = 1
	}
	for _, c := range cases {
		var got string
		if err := db.QueryRow(c.pragma).Scan(&got); err != nil {
			t.Errorf("%s: %v", c.pragma, err)
			continue
		}
		if !strings.EqualFold(got, c.wantContains) {
			t.Errorf("%s = %q, veut contient %q", c.pragma, got, c.wantContains)
		}
	}
}

func TestInTxCommitOnSuccess(t *testing.T) {
	t.Parallel()
	db := helperOpen(t)

	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	err := db.InTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO t VALUES (1)")
		return err
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count = %d, veut 1", count)
	}
}

func TestInTxRollbackOnError(t *testing.T) {
	t.Parallel()
	db := helperOpen(t)

	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("boom")
	err := db.InTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO t VALUES (1)"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("erreur retournee = %v, veut %v", err, sentinel)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("rollback failed, count = %d, veut 0", count)
	}
}

func TestInTxRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	db := helperOpen(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // annule avant même de démarrer

	err := db.InTx(ctx, func(_ *sql.Tx) error {
		t.Error("fn ne doit pas être appelée sur contexte annulé")
		return nil
	})
	if err == nil {
		t.Error("InTx doit retourner l'erreur du contexte")
	}
}

func TestOpenIsIdempotentSameFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db1.Close() }()
	if _, err := db1.Exec("CREATE TABLE t (v TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db1.Exec("INSERT INTO t VALUES ('hello')"); err != nil {
		t.Fatal(err)
	}
	if err := db1.Close(); err != nil {
		t.Fatal(err)
	}

	// Rouvrir le même fichier : les données doivent être là.
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()
	var v string
	if err := db2.QueryRow("SELECT v FROM t").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Errorf("v = %q, veut hello", v)
	}
}
