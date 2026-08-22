// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

// TestSinceRespecteLaBorne : la table n'est purgée par personne, et un
// client SSE qui se reconnecte sans Last-Event-ID demande tout depuis
// zéro. Sans LIMIT dans la requête, l'instance chargeait l'historique
// entier en mémoire sur une simple reconnexion.
func TestSinceRespecteLaBorne(t *testing.T) {
	t.Parallel()

	db, err := Open(filepath.Join(t.TempDir(), "borne.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo, err := NewEventsRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 50; i++ {
		if err := repo.Save(store.EventRecord{
			ID: uint64(i), Type: "test_event", At: at, DataJSON: `{"n":1}`,
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.Since(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Fatalf("%d events rendus, veut 10 — la borne n'est pas appliquée", len(got))
	}
	// Les plus anciens d'abord : un catch-up reprend où le client s'est
	// arrêté, il ne saute pas à la fin.
	if got[0].ID != 1 || got[9].ID != 10 {
		t.Errorf("plage rendue = [%d..%d], veut [1..10]", got[0].ID, got[9].ID)
	}

	// Un appelant qui demande zéro ligne en reçoit zéro, plutôt que
	// tout : une borne absente ne doit jamais valoir « sans limite ».
	if vide, err := repo.Since(0, 0); err != nil || len(vide) != 0 {
		t.Errorf("Since(0, 0) = %d events, %v — veut 0 event, nil", len(vide), err)
	}
	if vide, err := repo.Since(0, -1); err != nil || len(vide) != 0 {
		t.Errorf("Since(0, -1) = %d events, %v — veut 0 event, nil", len(vide), err)
	}
}
