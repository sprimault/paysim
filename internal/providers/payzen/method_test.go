// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"testing"
	"time"
)

func TestMaskPAN(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"1", "X"},
		{"1234", "XXXX"},
		{"12345", "X2345"},
		{"123456789", "XXXXX6789"},
		// PAN de 10 chiffres : les 6 premiers + les 4 derniers se
		// touchent, rien à masquer entre — sortie identique à l'entrée.
		// Cas d'école ; les vraies cartes font 13-19 chiffres.
		{"1234567890", "1234567890"},
		{"12345678901", "123456X8901"},
		{"4111111111111111", "411111XXXXXX1111"},
		{"345678901234567", "345678XXXXX4567"}, // AMEX 15 chiffres
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			if got := maskPAN(c.in); got != c.want {
				t.Errorf("maskPAN(%q) = %q, veut %q", c.in, got, c.want)
			}
		})
	}
}

func TestBrandFromBIN(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"4111111111111111", "VISA"},
		{"4000000000000002", "VISA"},
		{"5199999999999999", "MASTERCARD"},
		{"5555555555554444", "MASTERCARD"},
		{"2221000000000009", "MASTERCARD"}, // nouveau BIN Mastercard 2016+
		{"2720999999999999", "MASTERCARD"},
		{"371449635398431", "AMEX"},
		{"340000000000009", "AMEX"},
		{"6011111111111117", ""}, // Discover — non couvert
		{"", ""},
		{"4", ""}, // trop court
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			if got := BrandFromBIN(c.in); got != c.want {
				t.Errorf("BrandFromBIN(%q) = %q, veut %q", c.in, got, c.want)
			}
		})
	}
}

func TestIsLuhnValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pan  string
		want bool
	}{
		{"4111111111111111", true},  // PAN test Visa Luhn-valide
		{"4111111111111112", false}, // dernier chiffre modifié → Luhn KO
		{"5555555555554444", true},  // PAN test Mastercard
		{"371449635398431", true},   // PAN test Amex
		{"1234567890", false},
		{"", false},
		{"411A111111111111", false}, // caractère non numérique
	}
	for _, c := range cases {
		t.Run(c.pan, func(t *testing.T) {
			t.Parallel()
			if got := IsLuhnValid(c.pan); got != c.want {
				t.Errorf("IsLuhnValid(%q) = %v, veut %v", c.pan, got, c.want)
			}
		})
	}
}

func TestPaymentMethod_IsExpired(t *testing.T) {
	t.Parallel()
	// Date de référence : 15 juin 2026.
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		expMonth    int
		expYear     int
		wantExpired bool
	}{
		{"annee future", 1, 2027, false},
		{"annee passee", 12, 2025, true},
		{"meme annee mois futur", 12, 2026, false},
		{"meme annee mois passe", 5, 2026, true},
		{"meme annee meme mois", 6, 2026, false}, // encore valide « ce mois-ci »
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := &PaymentMethod{ExpiryMonth: c.expMonth, ExpiryYear: c.expYear}
			if got := m.IsExpired(now); got != c.wantExpired {
				t.Errorf("IsExpired(%v) = %v, veut %v", now, got, c.wantExpired)
			}
		})
	}
}

func TestNewPaymentMethod_brandDeduitDuBIN(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NewPaymentMethod("tok-1", "payzen", Card{
		PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2027,
	}, Customer{}, now)
	if m.Brand != "VISA" {
		t.Errorf("Brand = %q, veut VISA (déduit du BIN 4)", m.Brand)
	}
	if m.PANMasked != "411111XXXXXX1111" {
		t.Errorf("PANMasked = %q, veut 411111XXXXXX1111", m.PANMasked)
	}
	if m.PANFull != "4111111111111111" {
		t.Errorf("PANFull = %q, veut le PAN complet", m.PANFull)
	}
	if m.Revoked {
		t.Errorf("Revoked = true, veut false a la creation")
	}
	if !m.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, veut %v", m.CreatedAt, now)
	}
}

func TestNewPaymentMethod_brandExpliciteGardeLaMain(t *testing.T) {
	t.Parallel()
	// Un brand explicitement fourni ne se fait pas écraser par la
	// déduction — utile pour un scénario qui veut simuler une CB
	// co-marquée (Visa/CB en France) et privilégier CB.
	m := NewPaymentMethod("tok-2", "payzen", Card{
		PAN: "4111111111111111", Brand: "CB",
		ExpiryMonth: 6, ExpiryYear: 2027,
	}, Customer{}, time.Now())
	if m.Brand != "CB" {
		t.Errorf("Brand = %q, veut CB (explicite)", m.Brand)
	}
}

// TestNewPaymentMethod_attributsCarte verifie que les attributs
// descriptifs de la carte survivent a l'enrolement. Sans eux, le
// kr-answer ne pouvait qu'annoncer ses valeurs par defaut.
func TestNewPaymentMethod_attributsCarte(t *testing.T) {
	t.Parallel()
	pm := NewPaymentMethod("tok", "payzen", Card{
		PAN:             "4000001234562646",
		ExpiryMonth:     8,
		ExpiryYear:      2029,
		Brand:           "VISA",
		HolderName:      "DUPONT JEAN",
		Country:         "US",
		ProductCategory: "DEBIT",
		IssuerName:      "BANQUE DE TEST",
	}, Customer{}, time.Now().UTC())

	if pm.HolderName != "DUPONT JEAN" {
		t.Errorf("HolderName = %q", pm.HolderName)
	}
	if pm.Country != "US" {
		t.Errorf("Country = %q, veut US (carte etrangere simulable)", pm.Country)
	}
	if pm.ProductCategory != "DEBIT" {
		t.Errorf("ProductCategory = %q, veut DEBIT", pm.ProductCategory)
	}
	if pm.IssuerName != "BANQUE DE TEST" {
		t.Errorf("IssuerName = %q", pm.IssuerName)
	}
}

// TestPaymentMethodRecordRoundTrip verifie que les converters
// payzen <-> record ne perdent aucun attribut de carte.
func TestPaymentMethodRecordRoundTrip(t *testing.T) {
	t.Parallel()
	orig := NewPaymentMethod("tok", "payzen", Card{
		PAN:             "4000001234562646",
		ExpiryMonth:     8,
		ExpiryYear:      2029,
		Brand:           "VISA",
		HolderName:      "DUPONT JEAN",
		Country:         "US",
		ProductCategory: "DEBIT",
		IssuerName:      "BANQUE DE TEST",
	}, Customer{}, time.Now().UTC().Truncate(time.Second))

	back := recordToPayzenMethod(payzenMethodToRecord(orig))

	if *back != *orig {
		t.Errorf("round-trip altere le moyen :\n  avant = %+v\n  apres = %+v", *orig, *back)
	}
}
