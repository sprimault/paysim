// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package inmem

import (
	"fmt"
	"testing"

	"github.com/sprimault/paysim/internal/store"
)

// rec fabrique un enregistrement minimal : seul l'UUID compte pour les
// tests de rétention.
func rec(uuid string) *store.PaymentRecord {
	return &store.PaymentRecord{UUID: uuid, Provider: "payzen", OrderID: uuid}
}

// enregistrer insère n paiements nommés de façon prévisible, du plus
// ancien au plus récent.
func enregistrer(t *testing.T, r *PaymentsRepository, n int) {
	t.Helper()
	for i := range n {
		if err := r.Save(rec(fmt.Sprintf("uuid-%03d", i))); err != nil {
			t.Fatalf("Save %d : %v", i, err)
		}
	}
}

func compter(t *testing.T, r *PaymentsRepository) int {
	t.Helper()
	n, err := r.Count()
	if err != nil {
		t.Fatalf("Count : %v", err)
	}
	return n
}

func present(t *testing.T, r *PaymentsRepository, uuid string) bool {
	t.Helper()
	got, err := r.ByUUID(uuid)
	if err != nil {
		t.Fatalf("ByUUID %s : %v", uuid, err)
	}
	return got != nil
}

// TestRetention_plafondJamaisDepasse est la régression de base : la map
// grandissait sans borne, et PAYSIM_MAX_PAYMENTS ne dimensionnait en
// réalité que le canal de la file de livraison.
func TestRetention_plafondJamaisDepasse(t *testing.T) {
	t.Parallel()
	for _, max := range []int{1, 5, 100} {
		r := NewPaymentsRepository(max, nil)
		enregistrer(t, r, max*3)
		if got := compter(t, r); got != max {
			t.Errorf("max=%d : %d paiements retenus, veut %d", max, got, max)
		}
	}
}

// TestRetention_evinceLePlusAncienCree vérifie que l'éviction suit
// l'ordre de création et non celui de modification. Un tri par
// UpdatedAt — l'ordre dans lequel All() rend les paiements — évincerait
// le mauvais : ici uuid-000 est le plus récemment écrit au moment où le
// plafond est franchi.
func TestRetention_evinceLePlusAncienCree(t *testing.T) {
	t.Parallel()
	r := NewPaymentsRepository(2, nil)
	enregistrer(t, r, 2)

	// uuid-000 redevient le plus récemment modifié.
	if err := r.Save(rec("uuid-000")); err != nil {
		t.Fatalf("Save de mise a jour : %v", err)
	}
	if err := r.Save(rec("uuid-002")); err != nil {
		t.Fatalf("Save : %v", err)
	}

	if present(t, r, "uuid-000") {
		t.Error("uuid-000 retenu : l'eviction a suivi UpdatedAt, pas l'ordre de creation")
	}
	for _, u := range []string{"uuid-001", "uuid-002"} {
		if !present(t, r, u) {
			t.Errorf("%s evince a tort", u)
		}
	}
}

// TestRetention_miseAJourNEmpilePas couvre le piège principal : Save
// n'empile l'UUID qu'à la première écriture.
//
// L'assertion porte sur la file elle-même, et pas seulement sur ce que
// rend l'API. Empiler à chaque écriture donne le même résultat
// observable — les doublons sont sautés à l'éviction — mais fait enfler
// la file sans borne. Une instance qui borne sa map de paiements pour
// finir par accumuler des UUID ailleurs n'aurait rien réglé, et c'est
// invisible de l'extérieur : d'où le test en boîte blanche.
func TestRetention_miseAJourNEmpilePas(t *testing.T) {
	t.Parallel()
	r := NewPaymentsRepository(3, nil)
	enregistrer(t, r, 3)

	for range 50 {
		if err := r.Save(rec("uuid-001")); err != nil {
			t.Fatalf("Save de mise a jour : %v", err)
		}
	}

	if got := compter(t, r); got != 3 {
		t.Fatalf("%d paiements retenus apres 50 mises a jour, veut 3", got)
	}
	if len(r.ordre) != 3 {
		t.Errorf("file d'eviction = %d entrees apres 50 mises a jour, veut 3", len(r.ordre))
	}
	for _, u := range []string{"uuid-000", "uuid-001", "uuid-002"} {
		if !present(t, r, u) {
			t.Errorf("%s evince alors que le plafond n'a pas bouge", u)
		}
	}
}

// TestRetention_suppressionPuisDepassement vérifie que la file
// d'éviction reste cohérente avec la map quand un paiement disparaît
// par ailleurs. La file garde l'UUID mort ; il doit être sauté, pas
// compté comme une éviction — sinon un paiement vivant survit au
// plafond.
func TestRetention_suppressionPuisDepassement(t *testing.T) {
	t.Parallel()
	r := NewPaymentsRepository(3, nil)
	enregistrer(t, r, 3)

	if err := r.DeleteByUUID("uuid-000"); err != nil {
		t.Fatalf("DeleteByUUID : %v", err)
	}
	enregistrer(t, r, 6) // uuid-000 revient, puis 003 a 005

	if got := compter(t, r); got != 3 {
		t.Fatalf("%d paiements retenus, veut 3", got)
	}
	for _, u := range []string{"uuid-003", "uuid-004", "uuid-005"} {
		if !present(t, r, u) {
			t.Errorf("%s absent : la file d'eviction a derive de la map", u)
		}
	}
}

// TestRetention_sansPlafondRetientTout couvre les appelants qui ne
// veulent pas de borne — les tests des autres paquets, qui construisent
// le dépôt avec 0.
func TestRetention_sansPlafondRetientTout(t *testing.T) {
	t.Parallel()
	for _, max := range []int{0, -1} {
		r := NewPaymentsRepository(max, nil)
		enregistrer(t, r, 500)
		if got := compter(t, r); got != 500 {
			t.Errorf("max=%d : %d paiements retenus, veut 500", max, got)
		}
	}
}

// TestRetention_purgeRepartDeZero vérifie que DeleteAll remet la file
// d'éviction à plat. Sans cela, les UUID morts s'accumuleraient et la
// première éviction suivante en traverserait la totalité.
func TestRetention_purgeRepartDeZero(t *testing.T) {
	t.Parallel()
	r := NewPaymentsRepository(3, nil)
	enregistrer(t, r, 10)
	if _, err := r.DeleteAll(); err != nil {
		t.Fatalf("DeleteAll : %v", err)
	}
	if len(r.ordre) != 0 {
		t.Errorf("file d'eviction = %d entrees apres purge, veut 0", len(r.ordre))
	}
	enregistrer(t, r, 4)
	if got := compter(t, r); got != 3 {
		t.Errorf("%d paiements retenus apres purge, veut 3", got)
	}
}
