// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	sqlitepkg "github.com/sprimault/paysim/internal/store/sqlite"
)

// runHistoryContract exécute le même scénario sur une HistoryStore —
// garantit que MemoryHistory et SQLiteHistory sont interchangeables
// au bit près.
func runHistoryContract(t *testing.T, h HistoryStore) {
	t.Helper()
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	// Deux paiements se partagent les trois livraisons : c'est ce qui
	// permet de verifier que ByPayment separe, plutot que de retourner
	// tout ce qu'il trouve.
	paymentOf := map[string]string{"wh-1": "pay-a", "wh-2": "pay-b", "wh-3": "pay-a"}
	// Ajout de 3 records dans l'ordre chronologique.
	for i, id := range []string{"wh-1", "wh-2", "wh-3"} {
		rec := WebhookRecord{
			Webhook: Webhook{
				ID:          id,
				URL:         "http://x",
				Headers:     map[string]string{"h": "v"},
				Body:        []byte("payload-" + id),
				PaymentUUID: paymentOf[id],
				Attempts:    1,
				CreatedAt:   base.Add(time.Duration(i) * time.Second),
			},
			Status:      "delivered",
			StatusCode:  200,
			CompletedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := h.Add(rec); err != nil {
			t.Fatalf("Add %s: %v", id, err)
		}
	}

	// Recent doit renvoyer plus récent d'abord.
	recent := h.Recent(10)
	if len(recent) != 3 {
		t.Fatalf("Recent len = %d, veut 3", len(recent))
	}
	if recent[0].Webhook.ID != "wh-3" || recent[2].Webhook.ID != "wh-1" {
		t.Errorf("ordre = %s..%s, veut wh-3..wh-1",
			recent[0].Webhook.ID, recent[2].Webhook.ID)
	}

	// ByID direct.
	got, ok := h.ByID("wh-2")
	if !ok || got.Webhook.ID != "wh-2" {
		t.Errorf("ByID = %+v, ok=%v", got, ok)
	}
	if string(got.Webhook.Body) != "payload-wh-2" {
		t.Errorf("body corrompu")
	}

	// ByID inconnu.
	_, ok = h.ByID("inconnu")
	if ok {
		t.Error("ByID inconnu = ok true, veut false")
	}

	// ByPayment ne remonte que les livraisons du paiement demande, plus
	// recente d'abord. C'est le comportement dont l'UI depend pour
	// afficher le kr-answer du paiement ouvert et non celui du dernier
	// webhook de l'instance.
	ofA := h.ByPayment("pay-a", 10)
	if len(ofA) != 2 {
		t.Fatalf("ByPayment(pay-a) len = %d, veut 2", len(ofA))
	}
	if ofA[0].Webhook.ID != "wh-3" || ofA[1].Webhook.ID != "wh-1" {
		t.Errorf("ByPayment(pay-a) = %s,%s, veut wh-3,wh-1",
			ofA[0].Webhook.ID, ofA[1].Webhook.ID)
	}
	if ofB := h.ByPayment("pay-b", 10); len(ofB) != 1 || ofB[0].Webhook.ID != "wh-2" {
		t.Errorf("ByPayment(pay-b) = %+v, veut wh-2 seul", ofB)
	}
	if none := h.ByPayment("pay-inconnu", 10); len(none) != 0 {
		t.Errorf("ByPayment(inconnu) = %d, veut 0", len(none))
	}
	// Un uuid vide ne vaut pas « pas de filtre » : sinon un appelant qui
	// oublie de le passer croirait avoir filtre.
	if empty := h.ByPayment("", 10); len(empty) != 0 {
		t.Errorf("ByPayment(vide) = %d, veut 0", len(empty))
	}

	// CountsByPayment doit s'accorder avec ByPayment : la colonne de la
	// liste et la fiche du paiement lisent la meme chose, un ecart entre
	// les deux ferait douter de l'une comme de l'autre.
	counts := h.CountsByPayment()
	if counts["pay-a"] != len(ofA) || counts["pay-a"] != 2 {
		t.Errorf("counts[pay-a] = %d, veut 2", counts["pay-a"])
	}
	if counts["pay-b"] != 1 {
		t.Errorf("counts[pay-b] = %d, veut 1", counts["pay-b"])
	}
	if _, present := counts["pay-inconnu"]; present {
		t.Error("counts porte un paiement sans livraison")
	}
	// Les orphelins n'appartiennent a aucune ligne de la liste.
	if _, present := counts[""]; present {
		t.Error("counts compte les livraisons sans paiement rattache")
	}

	// DeleteAll purge tout.
	n, err := h.DeleteAll()
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if n != 3 {
		t.Errorf("deleted = %d, veut 3", n)
	}
	if r := h.Recent(10); len(r) != 0 {
		t.Errorf("apres purge : Recent len = %d", len(r))
	}
}

func TestMemoryHistoryContract(t *testing.T) {
	t.Parallel()
	runHistoryContract(t, NewMemoryHistory())
}

func TestSQLiteHistoryContract(t *testing.T) {
	t.Parallel()
	db, err := sqlitepkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo, err := sqlitepkg.NewWebhooksRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	runHistoryContract(t, NewSQLiteHistory(repo))
}

func TestMemoryHistoryRingWrapping(t *testing.T) {
	t.Parallel()
	h := NewMemoryHistory()
	// Remplir au-delà de la capacité — les plus anciens sont écrasés.
	for i := 0; i < historyCap+50; i++ {
		_ = h.Add(WebhookRecord{
			Webhook: Webhook{ID: "wh-" + string(rune('a'+i%26))},
		})
	}
	// Recent ne doit pas dépasser la capacité.
	if got := h.Recent(historyCap + 100); len(got) != historyCap {
		t.Errorf("Recent len = %d, veut %d", len(got), historyCap)
	}
}

// Le decompte ne peut pas depasser ce que le tampon retient : ce qui en
// est sorti n'est plus consultable, et annoncer un nombre plus grand que
// ce qu'on peut ouvrir serait un mensonge de plus qu'une approximation.
func TestMemoryHistoryCountsBorneesParLeTampon(t *testing.T) {
	t.Parallel()
	h := NewMemoryHistory()
	for i := 0; i < historyCap+50; i++ {
		_ = h.Add(WebhookRecord{
			Webhook: Webhook{ID: "wh-" + strconv.Itoa(i), PaymentUUID: "pay-a"},
		})
	}
	counts := h.CountsByPayment()
	if counts["pay-a"] != historyCap {
		t.Errorf("counts[pay-a] = %d, veut %d", counts["pay-a"], historyCap)
	}
	if got := len(h.ByPayment("pay-a", historyCap+100)); got != counts["pay-a"] {
		t.Errorf("ByPayment len = %d, counts = %d : les deux doivent s'accorder",
			got, counts["pay-a"])
	}
}
