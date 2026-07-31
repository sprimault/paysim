// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
)

// mockEnv est un remplacement de os.LookupEnv pour les tests.
type mockEnv map[string]string

func (m mockEnv) lookup(k string) (string, bool) {
	v, ok := m[k]
	return v, ok
}

// mockFS est un remplacement minimal de os.ReadFile pour les tests.
// Un chemin absent renvoie fs.ErrNotExist, la même sentinelle que
// os.ReadFile — les appelants peuvent errors.Is dessus s'ils veulent.
type mockFS map[string]string

func (m mockFS) read(path string) ([]byte, error) {
	v, ok := m[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(v), nil
}

// minEnv retourne le minimum requis pour un Load qui réussit.
// À enrichir dans chaque test qui a besoin d'options supplémentaires.
func minEnv() mockEnv {
	return mockEnv{
		"PAYSIM_PUBLIC_URL":   "https://paysim.example.com",
		"PAYSIM_CALLBACK_URL": "http://paysim.internal:8080",
	}
}

func TestLoadNominalMinimal(t *testing.T) {
	t.Parallel()
	cfg, err := loadFrom(minEnv().lookup, mockFS{}.read)
	if err != nil {
		t.Fatalf("erreur: %v", err)
	}
	if cfg.PublicURL.String() != "https://paysim.example.com" {
		t.Errorf("PublicURL = %s", cfg.PublicURL)
	}
	if cfg.CallbackURL.String() != "http://paysim.internal:8080" {
		t.Errorf("CallbackURL = %s", cfg.CallbackURL)
	}
	if cfg.BasePath != "" {
		t.Errorf("BasePath = %q, veut vide", cfg.BasePath)
	}
	if cfg.APIToken != "" {
		t.Errorf("APIToken = %q, veut vide", cfg.APIToken)
	}
	if cfg.MaxPayments != defaultMaxPayments {
		t.Errorf("MaxPayments = %d, veut %d", cfg.MaxPayments, defaultMaxPayments)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %s, veut info", cfg.LogLevel)
	}
}

func TestLoadNominalComplet(t *testing.T) {
	t.Parallel()
	env := mockEnv{
		"PAYSIM_PUBLIC_URL":       "https://paysim.example.com",
		"PAYSIM_CALLBACK_URL":     "http://paysim:8080",
		"PAYSIM_BASE_PATH":        "/paysim/",
		"PAYSIM_API_TOKEN":        "token-direct",
		"PAYSIM_MAX_PAYMENTS":     "500",
		"PAYSIM_LOG_LEVEL":        "debug",
		"PAYSIM_PAYZEN_HMAC_KEY":  "hmac-key-test",
	}
	cfg, err := loadFrom(env.lookup, mockFS{}.read)
	if err != nil {
		t.Fatalf("erreur: %v", err)
	}
	if cfg.BasePath != "/paysim" {
		t.Errorf("BasePath = %q, veut /paysim", cfg.BasePath)
	}
	if cfg.APIToken != "token-direct" {
		t.Errorf("APIToken = %q", cfg.APIToken)
	}
	if cfg.MaxPayments != 500 {
		t.Errorf("MaxPayments = %d", cfg.MaxPayments)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %s, veut debug", cfg.LogLevel)
	}
	if cfg.PayzenHMACKey != "hmac-key-test" {
		t.Errorf("PayzenHMACKey = %q", cfg.PayzenHMACKey)
	}
}

func TestLoadChaosDefaultAndOverride(t *testing.T) {
	t.Parallel()

	t.Run("defaut inerte", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadFrom(minEnv().lookup, mockFS{}.read)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ChaosLatencyMs != 0 || cfg.ChaosErrorRate != 0 {
			t.Errorf("chaos défaut = %d/%d, veut 0/0 (invariant 5)", cfg.ChaosLatencyMs, cfg.ChaosErrorRate)
		}
	})

	t.Run("activation", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_CHAOS_LATENCY_MS"] = "250"
		env["PAYSIM_CHAOS_ERROR_RATE"] = "30"
		cfg, err := loadFrom(env.lookup, mockFS{}.read)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ChaosLatencyMs != 250 || cfg.ChaosErrorRate != 30 {
			t.Errorf("chaos = %d/%d, veut 250/30", cfg.ChaosLatencyMs, cfg.ChaosErrorRate)
		}
	})

	t.Run("latency negative refusee", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_CHAOS_LATENCY_MS"] = "-10"
		_, err := loadFrom(env.lookup, mockFS{}.read)
		if err == nil {
			t.Error("latency négative acceptée")
		}
	})

	t.Run("error rate hors bornes refusee", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_CHAOS_ERROR_RATE"] = "150"
		_, err := loadFrom(env.lookup, mockFS{}.read)
		if err == nil {
			t.Error("error rate 150 accepté")
		}
	})
}

func TestLoadHTTPAddrDefaultAndOverride(t *testing.T) {
	t.Parallel()

	t.Run("defaut", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadFrom(minEnv().lookup, mockFS{}.read)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.HTTPAddr != ":8080" {
			t.Errorf("HTTPAddr defaut = %q, veut :8080", cfg.HTTPAddr)
		}
	})

	t.Run("surcharge", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_HTTP_ADDR"] = "127.0.0.1:9000"
		cfg, err := loadFrom(env.lookup, mockFS{}.read)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.HTTPAddr != "127.0.0.1:9000" {
			t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
		}
	})
}

func TestLoadPayzenHMACKeyFromFile(t *testing.T) {
	t.Parallel()
	env := minEnv()
	env["PAYSIM_PAYZEN_HMAC_KEY_FILE"] = "/run/secrets/hmac"
	fs := mockFS{"/run/secrets/hmac": "clef-depuis-fichier\n"}

	cfg, err := loadFrom(env.lookup, fs.read)
	if err != nil {
		t.Fatalf("erreur: %v", err)
	}
	if cfg.PayzenHMACKey != "clef-depuis-fichier" {
		t.Errorf("PayzenHMACKey = %q, veut clef-depuis-fichier (sans saut de ligne)", cfg.PayzenHMACKey)
	}
}

// TestLoadURLErrors couvre les cas d'échec des deux URL obligatoires,
// dans la même table parce que la validation est symétrique.
func TestLoadURLErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		env   mockEnv
		match string // sous-chaîne attendue dans le message d'erreur
	}{
		{
			name:  "public manquant",
			env:   mockEnv{"PAYSIM_CALLBACK_URL": "http://x:8080"},
			match: "PAYSIM_PUBLIC_URL manquant",
		},
		{
			name:  "public vide",
			env:   mockEnv{"PAYSIM_PUBLIC_URL": "", "PAYSIM_CALLBACK_URL": "http://x:8080"},
			match: "PAYSIM_PUBLIC_URL manquant",
		},
		{
			name:  "callback manquant",
			env:   mockEnv{"PAYSIM_PUBLIC_URL": "http://x"},
			match: "PAYSIM_CALLBACK_URL manquant",
		},
		{
			name:  "schema ftp",
			env:   mockEnv{"PAYSIM_PUBLIC_URL": "ftp://x", "PAYSIM_CALLBACK_URL": "http://x"},
			match: "http ou https",
		},
		{
			name:  "public relative",
			env:   mockEnv{"PAYSIM_PUBLIC_URL": "/paysim", "PAYSIM_CALLBACK_URL": "http://x"},
			match: "http ou https",
		},
		{
			name:  "url malformee",
			env:   mockEnv{"PAYSIM_PUBLIC_URL": "http://[bad", "PAYSIM_CALLBACK_URL": "http://x"},
			match: "PAYSIM_PUBLIC_URL invalide",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadFrom(c.env.lookup, mockFS{}.read)
			if err == nil {
				t.Fatalf("erreur attendue, obtenu nil")
			}
			if !strings.Contains(err.Error(), c.match) {
				t.Errorf("message %q ne contient pas %q", err.Error(), c.match)
			}
		})
	}
}

func TestLoadBasePathNormalisation(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":            "",
		"/":           "",
		"app":         "/app",
		"/app":        "/app",
		"/app/":       "/app",
		"/app/api":    "/app/api",
		"/app/api/":   "/app/api",
		"///app///":   "/app",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			env := minEnv()
			env["PAYSIM_BASE_PATH"] = in
			cfg, err := loadFrom(env.lookup, mockFS{}.read)
			if err != nil {
				t.Fatalf("erreur: %v", err)
			}
			if cfg.BasePath != want {
				t.Errorf("normalisation(%q) = %q, veut %q", in, cfg.BasePath, want)
			}
		})
	}
}

// TestLoadSecretFromFile vérifie le pattern _FILE : lecture, trim des
// espaces terminaux, gestion des cas d'erreur.
func TestLoadSecretFromFile(t *testing.T) {
	t.Parallel()

	t.Run("valeur lue depuis fichier", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_API_TOKEN_FILE"] = "/run/secrets/token"
		fs := mockFS{"/run/secrets/token": "s3cr3t\n"}
		cfg, err := loadFrom(env.lookup, fs.read)
		if err != nil {
			t.Fatalf("erreur: %v", err)
		}
		if cfg.APIToken != "s3cr3t" {
			t.Errorf("APIToken = %q, veut s3cr3t (sans saut de ligne)", cfg.APIToken)
		}
	})

	t.Run("conflit direct et fichier", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_API_TOKEN"] = "direct"
		env["PAYSIM_API_TOKEN_FILE"] = "/tmp/token"
		fs := mockFS{"/tmp/token": "fichier"}
		_, err := loadFrom(env.lookup, fs.read)
		if err == nil || !strings.Contains(err.Error(), "tous deux definis") {
			t.Errorf("attend erreur conflit, obtenu %v", err)
		}
	})

	t.Run("fichier introuvable", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_API_TOKEN_FILE"] = "/no/such/file"
		_, err := loadFrom(env.lookup, mockFS{}.read)
		if err == nil {
			t.Fatalf("erreur attendue")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("veut wrap de fs.ErrNotExist, obtenu %v", err)
		}
	})

	t.Run("nom de fichier vide", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_API_TOKEN_FILE"] = ""
		_, err := loadFrom(env.lookup, mockFS{}.read)
		if err == nil || !strings.Contains(err.Error(), "vide") {
			t.Errorf("attend erreur fichier vide, obtenu %v", err)
		}
	})
}

func TestLoadMaxPaymentsErrors(t *testing.T) {
	t.Parallel()
	cases := []string{"", "abc", "0", "-5", "3.14"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			env := minEnv()
			env["PAYSIM_MAX_PAYMENTS"] = raw
			_, err := loadFrom(env.lookup, mockFS{}.read)
			if err == nil {
				t.Fatalf("attend erreur pour %q", raw)
			}
			if !strings.Contains(err.Error(), "PAYSIM_MAX_PAYMENTS") {
				t.Errorf("message ne cite pas la variable: %v", err)
			}
		})
	}
}

func TestLoadLogLevel(t *testing.T) {
	t.Parallel()
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		" ERROR ": slog.LevelError,
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			env := minEnv()
			env["PAYSIM_LOG_LEVEL"] = raw
			cfg, err := loadFrom(env.lookup, mockFS{}.read)
			if err != nil {
				t.Fatalf("erreur: %v", err)
			}
			if cfg.LogLevel != want {
				t.Errorf("LogLevel(%q) = %s, veut %s", raw, cfg.LogLevel, want)
			}
		})
	}

	t.Run("invalide", func(t *testing.T) {
		t.Parallel()
		env := minEnv()
		env["PAYSIM_LOG_LEVEL"] = "verbose"
		_, err := loadFrom(env.lookup, mockFS{}.read)
		if err == nil || !strings.Contains(err.Error(), "PAYSIM_LOG_LEVEL") {
			t.Errorf("attend erreur, obtenu %v", err)
		}
	})
}
