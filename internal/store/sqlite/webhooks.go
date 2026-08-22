// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/sprimault/paysim/internal/store"
)

// WebhooksRepository est l'impl SQLite de store.WebhookRepository.
type WebhooksRepository struct {
	db *DB
}

// NewWebhooksRepository construit un WebhooksRepository et applique
// le schéma. Idempotent — ré-appliquable à chaque boot.
func NewWebhooksRepository(db *DB) (*WebhooksRepository, error) {
	r := &WebhooksRepository{db: db}
	if err := r.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("webhooks migrate: %w", err)
	}
	return r, nil
}

func (r *WebhooksRepository) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS webhooks (
			id TEXT PRIMARY KEY,
			url TEXT NOT NULL,
			headers_json TEXT NOT NULL DEFAULT '{}',
			body BLOB NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			outcome TEXT NOT NULL DEFAULT '',
			payment_uuid TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 0,
			error_msg TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			is_replay INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			completed_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_completed_at
			ON webhooks(completed_at DESC)`,
	}
	if err := appliquer(ctx, r.db, stmts...); err != nil {
		return err
	}
	// Bases antérieures à ces colonnes : on tente l'ALTER et on ignore
	// le "duplicate column", qui signale que l'état voulu est déjà
	// atteint. Les livraisons déjà historisées gardent une valeur vide
	// — ni l'outcome ni le paiement ne se reconstituent sans relire
	// chaque body, et le second n'y figure même pas toujours.
	alters := []string{
		`ALTER TABLE webhooks ADD COLUMN outcome TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE webhooks ADD COLUMN payment_uuid TEXT NOT NULL DEFAULT ''`,
		// Les livraisons déjà historisées passent pour des envois
		// d'origine : rien ne permet de reconnaître un rejeu après coup
		// sans se fier au format de son identifiant, ce qu'on refuse
		// justement de faire.
		`ALTER TABLE webhooks ADD COLUMN is_replay INTEGER NOT NULL DEFAULT 0`,
	}
	if err := ajouterColonnes(ctx, r.db, alters...); err != nil {
		return err
	}
	// Index créé après l'ALTER : sur une base ancienne, la colonne
	// n'existe pas encore au moment où le bloc stmts s'exécute.
	return appliquer(ctx, r.db,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_payment_uuid
			ON webhooks(payment_uuid, completed_at DESC)`)
}

// Save insère ou remplace un WebhookRecord.
func (r *WebhooksRepository) Save(rec *store.WebhookRecord) error {
	if rec == nil {
		return errors.New("Save(nil)")
	}
	if rec.ID == "" {
		return errors.New("Save: ID vide")
	}
	const upsert = `
		INSERT INTO webhooks (
			id, url, headers_json, body, status, outcome, payment_uuid,
			status_code, error_msg, attempts, is_replay, created_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			headers_json = excluded.headers_json,
			body = excluded.body,
			status = excluded.status,
			outcome = excluded.outcome,
			payment_uuid = excluded.payment_uuid,
			status_code = excluded.status_code,
			error_msg = excluded.error_msg,
			attempts = excluded.attempts,
			is_replay = excluded.is_replay,
			completed_at = excluded.completed_at
	`
	_, err := r.db.Exec(upsert,
		rec.ID, rec.URL, nonEmpty(rec.HeadersJSON), rec.Body,
		rec.Status, rec.Outcome, rec.PaymentUUID,
		rec.StatusCode, rec.ErrorMsg, rec.Attempts, rec.IsReplay,
		horodater(rec.CreatedAt),
		horodater(rec.CompletedAt),
	)
	return err
}

// webhookColumns fixe l'ordre de lecture, que scanWebhook suit à la
// lettre. Les trois requêtes de lecture le partagent : une colonne
// ajoutée ici sans l'être dans scanWebhook casse les trois d'un coup,
// ce qui vaut mieux qu'une seule silencieusement décalée.
const webhookColumns = `id, url, headers_json, body, status, outcome, payment_uuid,
	status_code, error_msg, attempts, is_replay, created_at, completed_at`

// Recent retourne les `limit` dernières entrées, plus récente d'abord.
func (r *WebhooksRepository) Recent(limit int) ([]*store.WebhookRecord, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT `+webhookColumns+`
		FROM webhooks
		ORDER BY completed_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	return collectWebhooks(rows)
}

// ByPayment retourne les livraisons d'un paiement, plus récente
// d'abord. Un uuid vide ne retourne rien plutôt que tout : les
// webhooks sans paiement rattaché ne forment pas un ensemble qu'on
// voudrait consulter, et le contraire ferait passer un filtre absent
// pour un filtre satisfait.
func (r *WebhooksRepository) ByPayment(paymentUUID string, limit int) ([]*store.WebhookRecord, error) {
	if paymentUUID == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT `+webhookColumns+`
		FROM webhooks
		WHERE payment_uuid = ?
		ORDER BY completed_at DESC, id DESC
		LIMIT ?
	`, paymentUUID, limit)
	if err != nil {
		return nil, err
	}
	return collectWebhooks(rows)
}

// CountsByPayment compte les livraisons par paiement, en une requête.
//
// L'agrégation est faite par la base plutôt qu'en rapatriant les
// enregistrements : la liste des paiements a besoin du seul nombre, et
// l'historique persisté peut compter des milliers d'entrées.
func (r *WebhooksRepository) CountsByPayment() (map[string]store.DeliveryCounts, error) {
	rows, err := r.db.Query(`
		SELECT payment_uuid, COUNT(*), SUM(is_replay)
		FROM webhooks
		WHERE payment_uuid <> ''
		GROUP BY payment_uuid
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]store.DeliveryCounts)
	for rows.Next() {
		var uuid string
		var c store.DeliveryCounts
		if err := rows.Scan(&uuid, &c.Total, &c.Replays); err != nil {
			return nil, err
		}
		counts[uuid] = c
	}
	return counts, rows.Err()
}

// collectWebhooks matérialise un curseur en enregistrements.
//
// Extrait des trois lectures qui parcourent les mêmes colonnes : sans
// cela, une requête ajoutée oublie facilement la fermeture du curseur ou
// le rows.Err() final, qui seul distingue une liste vide d'une lecture
// interrompue.
func collectWebhooks(rows *sql.Rows) ([]*store.WebhookRecord, error) {
	defer func() { _ = rows.Close() }()
	var out []*store.WebhookRecord
	for rows.Next() {
		rec, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ByID récupère un webhook par son ID.
func (r *WebhooksRepository) ByID(id string) (*store.WebhookRecord, error) {
	if id == "" {
		return nil, nil
	}
	row := r.db.QueryRow(`
		SELECT `+webhookColumns+`
		FROM webhooks WHERE id = ?
	`, id)
	return scanWebhook(row)
}

// DeleteAll purge la table.
func (r *WebhooksRepository) DeleteAll() (int, error) {
	res, err := r.db.Exec(`DELETE FROM webhooks`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// lotCascade borne le nombre d'UUID par instruction. La limite de
// paramètres de SQLite est très au-dessus (32 766) : ce qu'on borne
// ici, c'est la taille du SQL généré et la mémoire du plan, pas une
// contrainte du moteur.
const lotCascade = 500

// DeleteByPayment supprime les livraisons des paiements désignés.
//
// Hors transaction, un lot à la fois. Le paquet expose bien un
// helper transactionnel, mais il retient l'unique connexion du pool
// (SetMaxOpenConns(1)) sous un gabarit de délai court : une cascade de
// plusieurs milliers de livraisons y serait annulée en bloc. L'atomicité
// n'achèterait d'ailleurs rien — le paiement est déjà supprimé quand on
// arrive ici, et un lot en échec laisse des orphelines qu'un second
// appel rattrape.
//
// Le total rendu est celui réellement supprimé, même en cas d'échec en
// cours de route : l'appelant journalise l'erreur et annonce le partiel.
func (r *WebhooksRepository) DeleteByPayment(paymentUUIDs ...string) (int, error) {
	uuids := store.UUIDsDistincts(paymentUUIDs)
	if len(uuids) == 0 {
		return 0, nil
	}
	total := 0
	for debut := 0; debut < len(uuids); debut += lotCascade {
		fin := min(debut+lotCascade, len(uuids))
		lot := uuids[debut:fin]

		args := make([]any, len(lot))
		for i, u := range lot {
			args[i] = u
		}
		res, err := r.db.Exec(
			`DELETE FROM webhooks WHERE payment_uuid IN (`+placeholders(len(lot))+`)`, args...)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += int(n)
	}
	return total, nil
}

// DeleteAttached supprime les livraisons rattachées à un paiement et
// conserve les orphelines — même prédicat que CountsByPayment, pour que
// « ce qui compte pour un paiement » et « ce qui part avec les
// paiements » désignent exactement le même ensemble.
func (r *WebhooksRepository) DeleteAttached() (int, error) {
	res, err := r.db.Exec(`DELETE FROM webhooks WHERE payment_uuid <> ''`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// placeholders rend « ?, ?, ? » pour n paramètres.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// Close ferme la DB sous-jacente.
func (r *WebhooksRepository) Close() error {
	return r.db.Close()
}

func scanWebhook(sc paymentScanner) (*store.WebhookRecord, error) {
	var rec store.WebhookRecord
	var createdAtStr, completedAtStr string
	err := sc.Scan(
		&rec.ID, &rec.URL, &rec.HeadersJSON, &rec.Body,
		&rec.Status, &rec.Outcome, &rec.PaymentUUID,
		&rec.StatusCode, &rec.ErrorMsg, &rec.Attempts, &rec.IsReplay,
		&createdAtStr, &completedAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.CreatedAt, err = lireHorodatage("created_at", createdAtStr)
	if err != nil {
		return nil, err
	}
	rec.CompletedAt, err = lireHorodatage("completed_at", completedAtStr)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}
