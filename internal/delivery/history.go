// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"encoding/json"
	"sync"

	"github.com/sprimault/paysim/internal/store"
)

// HistoryStore est le contrat de persistance de l'historique des
// livraisons de webhooks. Deux implémentations :
//   - MemoryHistory (default) : ring buffer 200 entrées, aucune trace
//     entre redémarrages.
//   - SQLiteHistory (via store.WebhookRepository) : persistant sur
//     disque, pas de plafond automatique.
//
// L'interface expose une surface étroite alignée sur les besoins de
// la Queue et de l'API UI : ajouter une entrée, récupérer les N
// dernières, purger.
type HistoryStore interface {
	Add(rec WebhookRecord) error
	Recent(limit int) []WebhookRecord
	ByID(id string) (WebhookRecord, bool)
	ByPayment(paymentUUID string, limit int) []WebhookRecord
	CountsByPayment() map[string]store.DeliveryCounts
	DeleteAll() (int, error)

	// DeleteByPayment retire les livraisons des paiements désignés,
	// DeleteAttached toutes celles qui sont rattachées à un paiement.
	// La seconde sert la purge totale : lire les UUID puis les
	// supprimer laisserait passer un paiement créé entre les deux.
	DeleteByPayment(paymentUUIDs ...string) (int, error)
	DeleteAttached() (int, error)
}

// historyCap est la capacité du ring buffer mémoire — 200 couvre
// largement l'usage interactif. Persister l'historique sur SQLite
// permet de le pousser bien au-delà (voir SQLiteHistory).
const historyCap = 200

// MemoryHistory est un ring buffer en mémoire. Ancien comportement
// du Queue.history, extrait pour permettre la substitution SQLite.
type MemoryHistory struct {
	mu       sync.RWMutex
	buffer   [historyCap]WebhookRecord
	idx      int
	full     bool
}

// NewMemoryHistory instancie un historique en mémoire vide.
func NewMemoryHistory() *MemoryHistory {
	return &MemoryHistory{}
}

// Add ajoute un WebhookRecord au ring buffer. Écrase la plus ancienne
// entrée quand le buffer est plein.
func (m *MemoryHistory) Add(rec WebhookRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buffer[m.idx] = rec
	m.idx = (m.idx + 1) % historyCap
	if m.idx == 0 {
		m.full = true
	}
	return nil
}

// Recent retourne les `limit` dernières entrées, plus récente
// d'abord. Snapshot indépendant (copie).
func (m *MemoryHistory) Recent(limit int) []WebhookRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := m.idx
	if m.full {
		total = historyCap
	}
	if limit > total {
		limit = total
	}
	if limit <= 0 {
		return nil
	}

	out := make([]WebhookRecord, limit)
	for i := 0; i < limit; i++ {
		pos := (m.idx - 1 - i + historyCap) % historyCap
		out[i] = m.buffer[pos]
	}
	return out
}

// ByID cherche linéairement dans le ring buffer. Coût O(N) accepté :
// N=200 max, appelé sur les endpoints REST de détail webhook, pas
// sur le chemin critique.
func (m *MemoryHistory) ByID(id string) (WebhookRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := m.idx
	if m.full {
		total = historyCap
	}
	for i := 0; i < total; i++ {
		pos := (m.idx - 1 - i + historyCap) % historyCap
		if m.buffer[pos].Webhook.ID == id {
			return m.buffer[pos], true
		}
	}
	return WebhookRecord{}, false
}

// ByPayment retourne les livraisons rattachées à un paiement, plus
// récente d'abord. Même parcours linéaire que ByID, et pour la même
// raison : N vaut 200 au plus, et l'appel vient d'un endpoint REST de
// détail, pas du chemin de livraison.
//
// Un uuid vide ne retourne rien : les webhooks sans paiement rattaché
// — ceux d'avant ce champ, notamment — ne forment pas un ensemble
// qu'on voudrait afficher comme s'il en formait un.
func (m *MemoryHistory) ByPayment(paymentUUID string, limit int) []WebhookRecord {
	if paymentUUID == "" || limit <= 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := m.idx
	if m.full {
		total = historyCap
	}
	var out []WebhookRecord
	for i := 0; i < total && len(out) < limit; i++ {
		pos := (m.idx - 1 - i + historyCap) % historyCap
		if m.buffer[pos].Webhook.PaymentUUID == paymentUUID {
			out = append(out, m.buffer[pos])
		}
	}
	return out
}

// CountsByPayment compte les livraisons de chaque paiement présentes
// dans le tampon. Ce qui en est sorti n'est plus comptable — ni
// consultable, la fiche du paiement lisant le même tampon.
func (m *MemoryHistory) CountsByPayment() map[string]store.DeliveryCounts {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := m.idx
	if m.full {
		total = historyCap
	}
	counts := make(map[string]store.DeliveryCounts)
	for i := 0; i < total; i++ {
		uuid := m.buffer[i].Webhook.PaymentUUID
		if uuid == "" {
			continue
		}
		c := counts[uuid]
		c.Total++
		if m.buffer[i].Webhook.Replay {
			c.Replays++
		}
		counts[uuid] = c
	}
	return counts
}

// DeleteAll purge le ring buffer. Retourne le nombre d'entrées
// supprimées avant reset.
func (m *MemoryHistory) DeleteAll() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := m.idx
	if m.full {
		total = historyCap
	}
	m.buffer = [historyCap]WebhookRecord{}
	m.idx = 0
	m.full = false
	return total, nil
}

// DeleteByPayment supprime les livraisons des paiements désignés.
func (m *MemoryHistory) DeleteByPayment(paymentUUIDs ...string) (int, error) {
	uuids := store.UUIDsDistincts(paymentUUIDs)
	if len(uuids) == 0 {
		return 0, nil
	}
	vises := make(map[string]struct{}, len(uuids))
	for _, u := range uuids {
		vises[u] = struct{}{}
	}
	return m.compacter(func(rec WebhookRecord) bool {
		_, vise := vises[rec.Webhook.PaymentUUID]
		return !vise
	}), nil
}

// DeleteAttached supprime les livraisons rattachées à un paiement et
// conserve les orphelines — même prédicat que CountsByPayment.
func (m *MemoryHistory) DeleteAttached() (int, error) {
	return m.compacter(func(rec WebhookRecord) bool {
		return rec.Webhook.PaymentUUID == ""
	}), nil
}

// compacter reconstruit le tampon avec les seules entrées que garder
// retient, dans leur ordre chronologique, et rend le nombre supprimé.
//
// Compaction et non trous : Recent parcourt le tampon à rebours depuis
// idx en supposant que toute position vue porte une entrée. Y laisser
// des emplacements vides rendrait des WebhookRecord à zéro au milieu de
// la liste, avec des dates nulles et un identifiant vide.
//
// Sortie anticipée quand rien n'est supprimé : une purge de mille
// paiements passe ici mille fois, et réécrire deux cents entrées à
// chaque tour pour ne rien changer coûterait plus que la suppression
// elle-même. Elle préserve aussi `full`, que la compaction remet
// sinon à false.
//
// Effet de bord assumé sur `full` : après compaction le tampon repart
// non plein, donc les emplacements libérés retardent la prochaine
// éviction. C'est le comportement voulu — les entrées supprimées ne
// doivent pas continuer d'occuper la fenêtre de rétention.
func (m *MemoryHistory) compacter(garder func(WebhookRecord) bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := m.idx
	if m.full {
		total = historyCap
	}
	var neuf [historyCap]WebhookRecord
	n := 0
	for i := 0; i < total; i++ {
		pos := i
		if m.full {
			pos = (m.idx + i) % historyCap
		}
		if garder(m.buffer[pos]) {
			neuf[n] = m.buffer[pos]
			n++
		}
	}
	supprimes := total - n
	if supprimes == 0 {
		return 0
	}
	m.buffer = neuf
	m.idx = n
	m.full = false
	return supprimes
}

// -----------------------------------------------------------------------------
// SQLiteHistory : wrapper sur store.WebhookRepository
// -----------------------------------------------------------------------------

// SQLiteHistory persiste l'historique via un store.WebhookRepository.
// Les convertisseurs deliveryToRecord / recordToDelivery sérialisent
// les Headers en JSON.
type SQLiteHistory struct {
	repo store.WebhookRepository
}

// NewSQLiteHistory construit un SQLiteHistory autour du repository
// fourni. L'appelant garde la propriété du repository (main.go le
// ferme à shutdown).
func NewSQLiteHistory(repo store.WebhookRepository) *SQLiteHistory {
	return &SQLiteHistory{repo: repo}
}

// Add convertit et persiste via le repository.
func (s *SQLiteHistory) Add(rec WebhookRecord) error {
	sr, err := deliveryToRecord(rec)
	if err != nil {
		return err
	}
	return s.repo.Save(sr)
}

// Recent récupère les N derniers via le repository.
func (s *SQLiteHistory) Recent(limit int) []WebhookRecord {
	recs, err := s.repo.Recent(limit)
	if err != nil {
		// Signaler l'erreur remonte trop loin — on retourne vide,
		// l'appelant (API UI) verra une liste vide. Le vrai
		// diagnostic passe par les logs slog du repo.
		return nil
	}
	return convertAll(recs)
}

// ByPayment récupère les livraisons d'un paiement via le repository.
func (s *SQLiteHistory) ByPayment(paymentUUID string, limit int) []WebhookRecord {
	recs, err := s.repo.ByPayment(paymentUUID, limit)
	if err != nil {
		return nil
	}
	return convertAll(recs)
}

// CountsByPayment délègue l'agrégation au repository — la base compte
// mieux que nous, et sur tout ce qu'elle garde.
func (s *SQLiteHistory) CountsByPayment() map[string]store.DeliveryCounts {
	counts, err := s.repo.CountsByPayment()
	if err != nil {
		// Même parti que Recent : un décompte manquant laisse la page
		// utilisable, le diagnostic passe par les logs du repo.
		return nil
	}
	return counts
}

// convertAll traduit un lot d'enregistrements, en sautant ceux dont la
// conversion échoue : une entrée aux headers illisibles ne doit pas
// escamoter tout l'historique.
func convertAll(recs []*store.WebhookRecord) []WebhookRecord {
	out := make([]WebhookRecord, 0, len(recs))
	for _, sr := range recs {
		wr, err := recordToDelivery(sr)
		if err != nil {
			continue
		}
		out = append(out, wr)
	}
	return out
}

// ByID récupère et convertit.
func (s *SQLiteHistory) ByID(id string) (WebhookRecord, bool) {
	sr, err := s.repo.ByID(id)
	if err != nil || sr == nil {
		return WebhookRecord{}, false
	}
	wr, err := recordToDelivery(sr)
	if err != nil {
		return WebhookRecord{}, false
	}
	return wr, true
}

// DeleteAll délègue au repository.
func (s *SQLiteHistory) DeleteAll() (int, error) {
	return s.repo.DeleteAll()
}

// DeleteByPayment et DeleteAttached délèguent : le filtrage des UUID
// vaut pour les deux implémentations et vit donc dans le dépôt, pas ici.
func (s *SQLiteHistory) DeleteByPayment(paymentUUIDs ...string) (int, error) {
	return s.repo.DeleteByPayment(paymentUUIDs...)
}

func (s *SQLiteHistory) DeleteAttached() (int, error) {
	return s.repo.DeleteAttached()
}

// -----------------------------------------------------------------------------
// Converters delivery.WebhookRecord ⇄ store.WebhookRecord
// -----------------------------------------------------------------------------

func deliveryToRecord(w WebhookRecord) (*store.WebhookRecord, error) {
	headersJSON, err := json.Marshal(w.Webhook.Headers)
	if err != nil {
		return nil, err
	}
	return &store.WebhookRecord{
		ID:          w.Webhook.ID,
		URL:         w.Webhook.URL,
		HeadersJSON: string(headersJSON),
		Body:        w.Webhook.Body,
		Status:      w.Status,
		Outcome:     w.Webhook.Outcome,
		PaymentUUID: w.Webhook.PaymentUUID,
		StatusCode:  w.StatusCode,
		ErrorMsg:    w.ErrorMsg,
		Attempts:    w.Webhook.Attempts,
		IsReplay:    w.Webhook.Replay,
		CreatedAt:   w.Webhook.CreatedAt,
		CompletedAt: w.CompletedAt,
	}, nil
}

func recordToDelivery(sr *store.WebhookRecord) (WebhookRecord, error) {
	var headers map[string]string
	if sr.HeadersJSON != "" {
		if err := json.Unmarshal([]byte(sr.HeadersJSON), &headers); err != nil {
			return WebhookRecord{}, err
		}
	}
	return WebhookRecord{
		Webhook: Webhook{
			ID:        sr.ID,
			URL:       sr.URL,
			Headers:   headers,
			Body:        sr.Body,
			Outcome:     sr.Outcome,
			PaymentUUID: sr.PaymentUUID,
			Attempts:    sr.Attempts,
			Replay:      sr.IsReplay,
			CreatedAt:   sr.CreatedAt,
		},
		Status:      sr.Status,
		StatusCode:  sr.StatusCode,
		ErrorMsg:    sr.ErrorMsg,
		CompletedAt: sr.CompletedAt,
	}, nil
}
