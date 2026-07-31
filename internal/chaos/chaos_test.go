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
		{101, "UNPAID"},  // se termine par 01
		{1501, "UNPAID"}, // idem
		{102, ""},        // 02 pas encore mappé
		{103, ""},        // 03 est pour la latence, pas outcome
		{200, ""},
	}
	for _, c := range cases {
		if got := MagicOutcome(c.amount); got != c.want {
			t.Errorf("MagicOutcome(%d) = %q, veut %q", c.amount, got, c.want)
		}
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
