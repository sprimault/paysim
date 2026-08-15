// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"fmt"
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
	if counts["pay-a"].Total != len(ofA) || counts["pay-a"].Total != 2 {
		t.Errorf("counts[pay-a].Total = %d, veut 2", counts["pay-a"].Total)
	}
	if counts["pay-b"].Total != 1 {
		t.Errorf("counts[pay-b].Total = %d, veut 1", counts["pay-b"].Total)
	}
	// Aucun rejeu dans ce jeu : la part doit rester a zero, sans quoi
	// l'infobulle annoncerait des renvois qui n'ont pas eu lieu.
	if counts["pay-a"].Replays != 0 || counts["pay-b"].Replays != 0 {
		t.Errorf("Replays = %d/%d, veut 0/0",
			counts["pay-a"].Replays, counts["pay-b"].Replays)
	}
	if _, present := counts["pay-inconnu"]; present {
		t.Error("counts porte un paiement sans livraison")
	}
	// Les orphelins n'appartiennent a aucune ligne de la liste.
	if _, present := counts[""]; present {
		t.Error("counts compte les livraisons sans paiement rattache")
	}

	// Un rejeu compte dans le total et dans sa part. Le champ est
	// explicite : rien ne se deduit du format de l'identifiant.
	if err := h.Add(WebhookRecord{
		Webhook: Webhook{
			ID: "wh-4", URL: "http://x", Body: []byte("payload-wh-4"),
			PaymentUUID: "pay-b", Replay: true, Attempts: 1,
			CreatedAt: base.Add(4 * time.Second),
		},
		Status: "delivered", StatusCode: 200, CompletedAt: base.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("Add wh-4: %v", err)
	}
	apres := h.CountsByPayment()
	if apres["pay-b"].Total != 2 || apres["pay-b"].Replays != 1 {
		t.Errorf("apres rejeu : total=%d replays=%d, veut 2/1",
			apres["pay-b"].Total, apres["pay-b"].Replays)
	}
	if apres["pay-a"].Replays != 0 {
		t.Errorf("le rejeu d'un paiement a compte pour un autre")
	}

	// DeleteAll purge tout.
	n, err := h.DeleteAll()
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if n != 4 {
		t.Errorf("deleted = %d, veut 4", n)
	}
	if r := h.Recent(10); len(r) != 0 {
		t.Errorf("apres purge : Recent len = %d", len(r))
	}

	// Cascade. Repeuple sur un tampon vide, avec une orpheline : c'est
	// elle qui distingue « supprimer ce qui appartient a un paiement »
	// de « tout supprimer ».
	//
	// Place apres DeleteAll et non avant : le bloc precedent compte les
	// entrees, et en ajouter en amont ferait echouer son assertion sur
	// les deux implementations a la fois.
	ajouter := func(id, payment string, decalage int) {
		t.Helper()
		at := base.Add(time.Duration(100+decalage) * time.Second)
		if err := h.Add(WebhookRecord{
			Webhook: Webhook{
				ID: id, URL: "http://x", Body: []byte("payload-" + id),
				PaymentUUID: payment, Attempts: 1, CreatedAt: at,
			},
			Status: "delivered", StatusCode: 200, CompletedAt: at,
		}); err != nil {
			t.Fatalf("Add %s: %v", id, err)
		}
	}
	ajouter("wh-10", "pay-a", 0)
	ajouter("wh-11", "pay-b", 1)
	ajouter("wh-12", "", 2)

	// Un appel qui ne designe rien ne doit rien supprimer — surtout pas
	// l'orpheline, qu'un uuid vide laisse passer si la garde manque.
	for _, cas := range [][]string{{}, {""}, {"", ""}, {"inconnu"}} {
		if n, err := h.DeleteByPayment(cas...); err != nil || n != 0 {
			t.Errorf("DeleteByPayment(%v) = (%d, %v), veut (0, nil)", cas, n, err)
		}
	}
	if r := h.Recent(10); len(r) != 3 {
		t.Fatalf("un appel sans cible a supprime : Recent len = %d, veut 3", len(r))
	}

	n, err = h.DeleteByPayment("pay-a")
	if err != nil || n != 1 {
		t.Fatalf("DeleteByPayment(pay-a) = (%d, %v), veut (1, nil)", n, err)
	}
	if got := h.ByPayment("pay-a", 10); len(got) != 0 {
		t.Errorf("pay-a garde %d livraison(s)", len(got))
	}
	if got := h.ByPayment("pay-b", 10); len(got) != 1 {
		t.Errorf("pay-b a perdu ses livraisons : %d restante(s)", len(got))
	}
	// L'ordre doit rester decroissant apres compaction : c'est ce qui
	// casserait si le tampon gardait des trous.
	rest := h.Recent(10)
	if len(rest) != 2 {
		t.Fatalf("apres cascade : Recent len = %d, veut 2", len(rest))
	}
	if rest[0].CompletedAt.Before(rest[1].CompletedAt) {
		t.Errorf("ordre casse apres compaction : %v avant %v",
			rest[0].CompletedAt, rest[1].CompletedAt)
	}

	// DeleteAttached emporte ce qui reste de rattache, jamais l'orpheline.
	n, err = h.DeleteAttached()
	if err != nil || n != 1 {
		t.Fatalf("DeleteAttached = (%d, %v), veut (1, nil)", n, err)
	}
	rest = h.Recent(10)
	if len(rest) != 1 || rest[0].Webhook.ID != "wh-12" {
		t.Errorf("l'orpheline devait survivre, reste = %+v", rest)
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

// La compaction doit tenir sur un tampon qui a bouclé, cas où l'ordre
// logique ne suit plus l'ordre des positions. Un parcours naïf de 0 à
// idx-1 rendrait ici une liste dans le désordre et des entrées à zéro.
func TestMemoryHistoryDeleteByPaymentApresBouclage(t *testing.T) {
	t.Parallel()
	h := NewMemoryHistory()
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	// 250 entrées pour dépasser la capacité de 200 : le tampon a bouclé
	// et idx est au milieu quand la suppression arrive.
	for i := 0; i < 250; i++ {
		paiement := "pay-pair"
		if i%2 == 1 {
			paiement = "pay-impair"
		}
		at := base.Add(time.Duration(i) * time.Second)
		_ = h.Add(WebhookRecord{
			Webhook: Webhook{
				ID: fmt.Sprintf("wh-%03d", i), PaymentUUID: paiement,
				CreatedAt: at,
			},
			Status: "delivered", CompletedAt: at,
		})
	}

	n, err := h.DeleteByPayment("pay-pair")
	if err != nil {
		t.Fatalf("DeleteByPayment: %v", err)
	}
	restant := h.Recent(historyCap)
	if len(restant) != historyCap-n {
		t.Fatalf("Recent len = %d, veut %d apres %d suppressions",
			len(restant), historyCap-n, n)
	}

	for i, rec := range restant {
		if rec.Webhook.ID == "" || rec.CompletedAt.IsZero() {
			t.Fatalf("entree a zero en position %d : la compaction a laisse un trou", i)
		}
		if rec.Webhook.PaymentUUID != "pay-impair" {
			t.Errorf("position %d : %s a survecu", i, rec.Webhook.PaymentUUID)
		}
		if i > 0 && restant[i-1].CompletedAt.Before(rec.CompletedAt) {
			t.Fatalf("ordre casse en position %d", i)
		}
	}

	// Les deux lectures doivent voir le même tampon.
	if got := len(h.ByPayment("pay-impair", 500)); got != len(restant) {
		t.Errorf("ByPayment = %d, Recent = %d", got, len(restant))
	}
	if c := h.CountsByPayment(); c["pay-pair"].Total != 0 {
		t.Errorf("pay-pair compte encore %d livraisons", c["pay-pair"].Total)
	}
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
	if counts["pay-a"].Total != historyCap {
		t.Errorf("counts[pay-a].Total = %d, veut %d", counts["pay-a"].Total, historyCap)
	}
	if got := len(h.ByPayment("pay-a", historyCap+100)); got != counts["pay-a"].Total {
		t.Errorf("ByPayment len = %d, counts = %d : les deux doivent s'accorder",
			got, counts["pay-a"].Total)
	}
}
