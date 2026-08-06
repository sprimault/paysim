// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/store"
)

func buildMethodsRepo(t *testing.T) *PaymentMethodsRepository {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "methods.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	repo, err := NewPaymentMethodsRepository(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewPaymentMethodsRepository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close(); _ = db.Close() })
	return repo
}

func sampleMethod(token, provider string) *store.PaymentMethodRecord {
	return &store.PaymentMethodRecord{
		Token:            token,
		Provider:         provider,
		PANFull:          "4111111111111111",
		PANMasked:        "411111XXXXXX1111",
		Brand:            "VISA",
		ExpiryMonth:      12,
		ExpiryYear:       2027,
		Revoked:          false,
		MetadataJSON:     `{}`,
		ProviderDataJSON: `{}`,
		CreatedAt:        time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}
}

func TestPaymentMethods_saveAndByToken(t *testing.T) {
	t.Parallel()
	repo := buildMethodsRepo(t)
	if err := repo.Save(sampleMethod("pmt-1", "payzen")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.ByToken("pmt-1")
	if err != nil {
		t.Fatalf("ByToken: %v", err)
	}
	if got == nil {
		t.Fatal("ByToken = nil, veut le record")
	}
	if got.PANFull != "4111111111111111" || got.PANMasked != "411111XXXXXX1111" {
		t.Errorf("PAN full/masked = %q/%q", got.PANFull, got.PANMasked)
	}
	if got.Brand != "VISA" || got.ExpiryMonth != 12 || got.ExpiryYear != 2027 {
		t.Errorf("Brand/Expiry = %q/%d/%d", got.Brand, got.ExpiryMonth, got.ExpiryYear)
	}
	if got.Revoked {
		t.Errorf("Revoked = true, veut false a la creation")
	}
}

func TestPaymentMethods_byTokenInconnu(t *testing.T) {
	t.Parallel()
	repo := buildMethodsRepo(t)
	if got, err := repo.ByToken("does-not-exist"); err != nil || got != nil {
		t.Errorf("ByToken(inconnu) = %+v, %v ; veut nil, nil", got, err)
	}
}

func TestPaymentMethods_revoke(t *testing.T) {
	t.Parallel()
	repo := buildMethodsRepo(t)
	_ = repo.Save(sampleMethod("pmt-1", "payzen"))
	if err := repo.Revoke("pmt-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, _ := repo.ByToken("pmt-1")
	if !got.Revoked {
		t.Errorf("apres Revoke, Revoked = false, veut true")
	}
	// Idempotent : revoke deux fois ne casse pas.
	if err := repo.Revoke("pmt-1"); err != nil {
		t.Errorf("Revoke second = %v, veut nil", err)
	}
	// Idempotent aussi sur token inconnu — l'etat demande (« ce token
	// n'est plus utilisable ») est atteint pour un token inexistant.
	if err := repo.Revoke("inconnu"); err != nil {
		t.Errorf("Revoke inconnu = %v, veut nil", err)
	}
}

func TestPaymentMethods_saveEcrase(t *testing.T) {
	t.Parallel()
	repo := buildMethodsRepo(t)
	rec := sampleMethod("pmt-1", "payzen")
	_ = repo.Save(rec)
	rec.Brand = "MASTERCARD"
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save maj: %v", err)
	}
	got, _ := repo.ByToken("pmt-1")
	if got.Brand != "MASTERCARD" {
		t.Errorf("Brand = %q, veut MASTERCARD apres upsert", got.Brand)
	}
}

func TestPaymentMethods_byProvider(t *testing.T) {
	t.Parallel()
	repo := buildMethodsRepo(t)
	_ = repo.Save(sampleMethod("p1", "payzen"))
	_ = repo.Save(sampleMethod("p2", "payzen"))
	_ = repo.Save(sampleMethod("s1", "stripe"))

	payzen, _ := repo.ByProvider("payzen")
	if len(payzen) != 2 {
		t.Errorf("payzen = %d, veut 2", len(payzen))
	}
	stripe, _ := repo.ByProvider("stripe")
	if len(stripe) != 1 {
		t.Errorf("stripe = %d, veut 1", len(stripe))
	}
	inconnu, _ := repo.ByProvider("adyen")
	if len(inconnu) != 0 {
		t.Errorf("adyen = %d, veut 0", len(inconnu))
	}
}

func TestPaymentMethods_count(t *testing.T) {
	t.Parallel()
	repo := buildMethodsRepo(t)
	if n, _ := repo.Count(); n != 0 {
		t.Errorf("Count initial = %d, veut 0", n)
	}
	_ = repo.Save(sampleMethod("p1", "payzen"))
	_ = repo.Save(sampleMethod("p2", "payzen"))
	if n, _ := repo.Count(); n != 2 {
		t.Errorf("Count = %d, veut 2", n)
	}
}

// TestPaymentMethods_migrateExistingTable exerce le chemin ALTER TABLE :
// une base créée avant l'ajout des attributs de carte doit se migrer au
// démarrage sans perdre ses lignes. Les tests qui partent d'une base
// neuve passent tous par CREATE TABLE et ne couvrent jamais ce cas —
// or c'est celui de toute instance déjà déployée.
func TestPaymentMethods_migrateExistingTable(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Schéma tel qu'il existait avant holder_name / country /
	// product_category / issuer_name.
	const legacySchema = `CREATE TABLE payment_methods (
		token TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		pan_full TEXT NOT NULL,
		pan_masked TEXT NOT NULL,
		brand TEXT NOT NULL DEFAULT '',
		expiry_month INTEGER NOT NULL,
		expiry_year INTEGER NOT NULL,
		revoked INTEGER NOT NULL DEFAULT 0,
		metadata_json TEXT NOT NULL DEFAULT '{}',
		provider_data_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL
	)`
	if _, err := db.ExecContext(t.Context(), legacySchema); err != nil {
		t.Fatalf("creation schema ancien: %v", err)
	}
	const insert = `INSERT INTO payment_methods (
		token, provider, pan_full, pan_masked, brand,
		expiry_month, expiry_year, revoked,
		metadata_json, provider_data_json, created_at
	) VALUES ('pmt-legacy', 'payzen', '4111111111111111', '411111XXXXXX1111',
		'VISA', 12, 2027, 0, '{}', '{}', '2026-08-02T10:00:00Z')`
	if _, err := db.ExecContext(t.Context(), insert); err != nil {
		t.Fatalf("insertion ligne ancienne: %v", err)
	}

	repo, err := NewPaymentMethodsRepository(db)
	if err != nil {
		t.Fatalf("migration sur base existante: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	got, err := repo.ByToken("pmt-legacy")
	if err != nil {
		t.Fatalf("ByToken apres migration: %v", err)
	}
	if got == nil {
		t.Fatal("la ligne preexistante a disparu apres migration")
	}
	if got.PANMasked != "411111XXXXXX1111" || got.Brand != "VISA" {
		t.Errorf("donnees alterees : PANMasked=%q Brand=%q", got.PANMasked, got.Brand)
	}
	// Les nouvelles colonnes existent et valent la chaîne vide.
	if got.HolderName != "" || got.Country != "" || got.ProductCategory != "" || got.IssuerName != "" {
		t.Errorf("nouvelles colonnes non vides sur une ligne ancienne : %q %q %q %q",
			got.HolderName, got.Country, got.ProductCategory, got.IssuerName)
	}

	// La migration doit rester idempotente : un second passage ne
	// casse pas (c'est ce qui arrive à chaque redémarrage).
	if _, err := NewPaymentMethodsRepository(db); err != nil {
		t.Fatalf("seconde migration non idempotente: %v", err)
	}
}
