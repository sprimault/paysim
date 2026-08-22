// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func baseDeTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// appliquer interrompt à la première erreur : un schéma à moitié appliqué
// est un piège plus coûteux qu'un démarrage refusé.
func TestAppliquerInterromptEtNommeLeStatementFautif(t *testing.T) {
	t.Parallel()
	db := baseDeTest(t)
	ctx := context.Background()

	err := appliquer(ctx, db,
		`CREATE TABLE bonne (id TEXT PRIMARY KEY)`,
		`CECI N'EST PAS DU SQL`,
		`CREATE TABLE jamais_creee (id TEXT PRIMARY KEY)`,
	)
	if err == nil {
		t.Fatal("un statement invalide doit interrompre la migration")
	}
	if !strings.Contains(err.Error(), "CECI") {
		t.Errorf("le message doit citer le statement fautif : %v", err)
	}

	// La table qui suivait ne doit pas exister : on s'arrête, on ne
	// continue pas sur un schéma partiel.
	var n int
	row := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='jamais_creee'`)
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("les statements suivant l'échec ne doivent pas s'exécuter")
	}
}

// C'est la propriété qui rend la migration rejouable : relancer le même
// ALTER sur une base déjà à jour ne doit rien signaler, puisque l'état
// demandé est atteint.
func TestAjouterColonnesToleraLaColonneDejaPresente(t *testing.T) {
	t.Parallel()
	db := baseDeTest(t)
	ctx := context.Background()

	if err := appliquer(ctx, db, `CREATE TABLE t (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	ajout := []string{
		`ALTER TABLE t ADD COLUMN a TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE t ADD COLUMN b INTEGER NOT NULL DEFAULT 0`,
	}
	if err := ajouterColonnes(ctx, db, ajout...); err != nil {
		t.Fatalf("premier passage : %v", err)
	}
	// Deuxième passage : c'est le démarrage suivant sur la même base.
	if err := ajouterColonnes(ctx, db, ajout...); err != nil {
		t.Fatalf("second passage, les colonnes existent déjà : %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO t (id, a, b) VALUES ('x', 'y', 1)`); err != nil {
		t.Errorf("les colonnes ajoutées doivent être utilisables : %v", err)
	}
}

// La tolérance ne vaut que pour la colonne déjà présente : toute autre
// erreur doit remonter, sous peine de laisser passer une table absente.
func TestAjouterColonnesPropageLesAutresErreurs(t *testing.T) {
	t.Parallel()
	db := baseDeTest(t)

	err := ajouterColonnes(context.Background(), db,
		`ALTER TABLE table_inexistante ADD COLUMN a TEXT NOT NULL DEFAULT ''`)
	if err == nil {
		t.Fatal("un ALTER sur une table absente doit remonter")
	}
	if !strings.Contains(err.Error(), "table_inexistante") {
		t.Errorf("le message doit citer le statement : %v", err)
	}
}
