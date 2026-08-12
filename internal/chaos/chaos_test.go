// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package chaos

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/format"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestNilChaosIsInert prouve l'invariant 5 pour un récepteur nil :
// aucune méthode ne doit avoir d'effet observable.
func TestNilChaosIsInert(t *testing.T) {
	t.Parallel()
	var c *Chaos // nil

	// Sleep sans extra : rien.
	start := time.Now()
	c.Sleep(context.Background(), 0)
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("Sleep(0) sur nil = %v, veut ~0", elapsed)
	}

	// ShouldFail sur nil : toujours false.
	if c.ShouldFail() {
		t.Error("ShouldFail() sur nil = true, veut false")
	}

	// Middleware sur nil : renvoie next inchangé.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := c.Middleware(handler)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Middleware nil = %d, veut 200", rec.Code)
	}
}

// TestZeroConfigIsInert prouve l'invariant 5 pour un Chaos non-nil
// avec Config zéro : aucun effet observable.
func TestZeroConfigIsInert(t *testing.T) {
	t.Parallel()
	c := New(Config{}, discardLogger())

	start := time.Now()
	c.Sleep(context.Background(), 0)
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("Sleep(0) sur zero config = %v", elapsed)
	}

	// ErrorRate=0 : ShouldFail toujours false, même sur 1000 essais.
	for i := 0; i < 1000; i++ {
		if c.ShouldFail() {
			t.Fatalf("ShouldFail = true sur essai %d avec ErrorRate=0", i)
		}
	}
}

func TestSleepAppliesConfiguredLatency(t *testing.T) {
	t.Parallel()
	c := New(Config{LatencyMs: 100}, discardLogger())

	start := time.Now()
	c.Sleep(context.Background(), 0)
	elapsed := time.Since(start)
	if elapsed < 90*time.Millisecond {
		t.Errorf("Sleep(100ms config) = %v, veut >= 90ms", elapsed)
	}
}

func TestSleepAppliesExtraLatency(t *testing.T) {
	t.Parallel()
	c := New(Config{LatencyMs: 50}, discardLogger())

	start := time.Now()
	c.Sleep(context.Background(), 100) // total attendu : 150ms
	elapsed := time.Since(start)
	if elapsed < 140*time.Millisecond {
		t.Errorf("Sleep(50+100ms) = %v, veut >= 140ms", elapsed)
	}
}

func TestSleepInterruptibleByContext(t *testing.T) {
	t.Parallel()
	// Latence configurée à 5s, mais on cancel après 50ms — Sleep doit
	// rendre la main immédiatement.
	c := New(Config{LatencyMs: 5000}, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	c.Sleep(ctx, 0)
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Sleep interrompu = %v, veut < 200ms", elapsed)
	}
}

func TestShouldFailRespectsErrorRate(t *testing.T) {
	t.Parallel()
	// ErrorRate=100 : toujours true.
	c100 := New(Config{ErrorRate: 100}, discardLogger())
	for i := 0; i < 100; i++ {
		if !c100.ShouldFail() {
			t.Fatalf("ErrorRate=100 : ShouldFail = false sur essai %d", i)
		}
	}
}

func TestShouldFailPartialRate(t *testing.T) {
	t.Parallel()
	// ErrorRate=50 : distribution ~50%. On tolère large sur 10 000 essais.
	c := New(Config{ErrorRate: 50}, discardLogger())
	fails := 0
	for i := 0; i < 10_000; i++ {
		if c.ShouldFail() {
			fails++
		}
	}
	if fails < 4000 || fails > 6000 {
		t.Errorf("ErrorRate=50 sur 10000 essais : %d échecs, attendu ~5000 (marge 40-60%%)", fails)
	}
}

func TestNewClampsErrorRate(t *testing.T) {
	t.Parallel()
	// Valeur > 100 doit être ramenée à 100, négative à 0.
	c := New(Config{ErrorRate: 250}, discardLogger())
	if c.cfg.ErrorRate != 100 {
		t.Errorf("ErrorRate 250 → %d, veut 100", c.cfg.ErrorRate)
	}
	cNeg := New(Config{ErrorRate: -5}, discardLogger())
	if cNeg.cfg.ErrorRate != 0 {
		t.Errorf("ErrorRate -5 → %d, veut 0", cNeg.cfg.ErrorRate)
	}
}

func TestMiddlewareInjectsError(t *testing.T) {
	t.Parallel()
	c := New(Config{ErrorRate: 100}, discardLogger())

	wrapped := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Middleware avec ErrorRate=100 → %d, veut 500", rec.Code)
	}
}

func TestMiddlewareAppliesLatency(t *testing.T) {
	t.Parallel()
	c := New(Config{LatencyMs: 100}, discardLogger())

	wrapped := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	start := time.Now()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	elapsed := time.Since(start)

	if elapsed < 90*time.Millisecond {
		t.Errorf("Middleware LatencyMs=100 → %v, veut >= 90ms", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Code = %d, veut 200 (latence ne doit pas changer le code)", rec.Code)
	}
}

func TestMagicOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		amount format.Amount
		want   string
	}{
		{100, ""},        // 100 → pas de magic
		{101, "UNPAID"},  // 01 → provision insuffisante
		{1501, "UNPAID"}, // idem, quel que soit l'ordre de grandeur
		{102, "UNPAID"},  // 02 → carte volée
		{104, "UNPAID"},  // 04 → émetteur inaccessible
		{103, ""},        // 03 est pour la latence, pas pour l'issue
		{200, ""},
	}
	for _, c := range cases {
		if got := MagicOutcome(c.amount); got != c.want {
			t.Errorf("MagicOutcome(%d) = %q, veut %q", c.amount, got, c.want)
		}
	}
}

func TestParseHeader(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want Overrides
	}{
		{"vide", "", Overrides{}},
		{"latency seule", "latency=1500", Overrides{LatencyMs: 1500}},
		{"status seul", "status=503", Overrides{Status: 503}},
		{"les deux", "latency=200&status=500", Overrides{LatencyMs: 200, Status: 500}},
		{"cle inconnue ignoree", "unknown=42&latency=100", Overrides{LatencyMs: 100}},
		{"latency invalide ignoree", "latency=abc", Overrides{}},
		{"status hors bornes ignore", "status=200", Overrides{}}, // < 400 refusé
		{"status hors bornes haut", "status=700", Overrides{}},
		{"latency negative ignoree", "latency=-10", Overrides{}},
		{"format degrade partiel", "latency=100&status", Overrides{LatencyMs: 100}},
		{"espaces autour", "latency = 200 & status = 500 ", Overrides{LatencyMs: 200, Status: 500}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := ParseHeader(c.in)
			if got != c.want {
				t.Errorf("ParseHeader(%q) = %+v, veut %+v", c.in, got, c.want)
			}
		})
	}
}

func TestMiddlewareHeaderInjectsStatus(t *testing.T) {
	t.Parallel()
	// Chaos nil : même sans config statique, le header agit.
	var c *Chaos

	wrapped := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Paysim-Chaos", "status=503")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, veut 503", rec.Code)
	}
}

func TestMiddlewareHeaderAppliesLatency(t *testing.T) {
	t.Parallel()
	var c *Chaos
	wrapped := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Paysim-Chaos", "latency=120")
	rec := httptest.NewRecorder()
	start := time.Now()
	wrapped.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed < 110*time.Millisecond {
		t.Errorf("latency header = %v, veut >= 110ms", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, veut 200 (latence n'affecte pas le code)", rec.Code)
	}
}

func TestMiddlewareHeaderOverridesConfig(t *testing.T) {
	t.Parallel()
	// Config statique : ErrorRate 100 (500 systématique).
	// Header : status=200 non forcé (juste latency).
	// Attendu : le header prime, pas de 500 injecté par config.
	c := New(Config{ErrorRate: 100, LatencyMs: 5000}, discardLogger())

	wrapped := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Paysim-Chaos", "latency=50")
	rec := httptest.NewRecorder()
	start := time.Now()
	wrapped.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, veut 200 (config ErrorRate 100 doit être ignoré par header)", rec.Code)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %v, veut < 500ms (config LatencyMs=5000 doit être ignoré)", elapsed)
	}
}

func TestMiddlewareHeaderIgnoredIfEmpty(t *testing.T) {
	t.Parallel()
	// Header absent : la config statique s'applique normalement.
	c := New(Config{ErrorRate: 100}, discardLogger())
	wrapped := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("sans header : code = %d, veut 500 (config ErrorRate 100)", rec.Code)
	}
}

func TestMagicLatencyMs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		amount format.Amount
		want   int
	}{
		{100, 0},
		{103, 30_000}, // se termine par 03
		{1503, 30_000},
		{101, 0}, // 01 est pour outcome, pas latence
		{200, 0},
	}
	for _, c := range cases {
		if got := MagicLatencyMs(c.amount); got != c.want {
			t.Errorf("MagicLatencyMs(%d) = %d, veut %d", c.amount, got, c.want)
		}
	}
}

// Les trois montants magiques refusent tous, mais avec des motifs
// distincts : c'est cette différence qui permet à un marchand d'écrire
// sa logique de reconduction et de la tester.
func TestMagicDeclineReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		amount   format.Amount
		wantCode string
	}{
		{101, "51"},  // provision insuffisante
		{1501, "51"}, // quel que soit l'ordre de grandeur
		{102, "43"},  // carte volée
		{104, "91"},  // émetteur inaccessible
		{103, ""},    // latence : refuse pas, donc pas de motif
		{100, ""},    // montant ordinaire
	}
	for _, c := range cases {
		if got := MagicDeclineReason(c.amount).Code; got != c.wantCode {
			t.Errorf("MagicDeclineReason(%d).Code = %q, veut %q", c.amount, got, c.wantCode)
		}
	}
}

// Le montant est imposé par l'abonnement sur un prélèvement récurrent :
// le PAN est alors le seul levier pour choisir un motif.
func TestDeclineReasonForPAN(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"4000000000000002": "51",
		"5105105105105100": "43",
		"2223000000000007": "05",
		"378282000000008":  "57",
		"4111111111111111": "", // PAN ordinaire, aucun motif
	}
	for pan, want := range cases {
		if got := DeclineReasonForPAN(pan).Code; got != want {
			t.Errorf("DeclineReasonForPAN(%s).Code = %q, veut %q", pan, got, want)
		}
	}
}

// Ce qui fait échouer une vérification sans débit tient au motif, pas
// au chemin : un statut de carte vaut quel que soit le montant, une
// provision ne s'interroge qu'à partir d'un montant.
//
// La distinction n'est pas propre à PayZen : elle porte sur des codes
// ISO 8583, ceux que l'acquéreur remonte. Un enregistrement de moyen
// chez n'importe quel fournisseur la rencontrera à l'identique.
func TestRefuseUneVerification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		nom    string
		reason DeclineReason
		want   bool
	}{
		{"opposition", ReasonStolenCard, true},
		{"refus emetteur", ReasonDoNotHonour, true},
		{"operation non permise", ReasonNotPermitted, true},
		// Une carte à découvert s'enrôle : la vérification n'engage
		// aucun montant, donc n'interroge pas le solde. Elle refusera
		// au premier débit, et c'est le seul levier qui produise un
		// abonnement dont les échéances refusent pour ce motif.
		{"provision insuffisante", ReasonInsufficientFunds, false},
		// Une panne d'émetteur est transitoire : elle ne dit rien de la
		// carte, et refuser l'enrôlement pour ça reviendrait à traiter
		// un incident réseau comme un défaut du porteur.
		{"emetteur inaccessible", ReasonIssuerUnavailable, false},
		{"aucun motif", DeclineReason{}, false},
	}
	for _, c := range cases {
		t.Run(c.nom, func(t *testing.T) {
			if got := RefuseUneVerification(c.reason); got != c.want {
				t.Errorf("RefuseUneVerification(%q) = %v, veut %v", c.reason.Code, got, c.want)
			}
		})
	}
}

// Tout PAN de refus doit porter un motif, sans quoi le refus reste muet
// sur le chemin récurrent — celui-là même qu'on veut rendre testable.
func TestChaqueTestPANPorteUnMotif(t *testing.T) {
	t.Parallel()
	for pan := range declinedTestPANs {
		if DeclineReasonForPAN(pan).Code == "" {
			t.Errorf("le PAN de refus %s n'a aucun motif associe", pan)
		}
	}
}
