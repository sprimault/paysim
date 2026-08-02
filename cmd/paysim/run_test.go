// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// scenarioServer est un fake serveur reproduit ici plutôt qu'importé
// depuis internal/scenarios (le fake y est en _test.go, non exporté).
// Suffisamment complet pour valider les codes retour de runCommand
// sur les 3 catégories : succès, assertion échouée, erreur d'exécution.
type scenarioServer struct {
	mu       sync.Mutex
	payments map[string]string
	webhooks []map[string]any
}

func newScenarioServer(t *testing.T) *httptest.Server {
	t.Helper()
	fs := &scenarioServer{payments: make(map[string]string)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /paysim/api/v1/payments", fs.create)
	mux.HandleFunc("POST /paysim/api/v1/payments/{uuid}/simulate", fs.simulate)
	mux.HandleFunc("GET /paysim/api/v1/payments/{uuid}", fs.get)
	mux.HandleFunc("GET /paysim/api/v1/webhooks", fs.list)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (s *scenarioServer) create(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uuid := "uuid-" + time.Now().UTC().Format("150405.000000000")
	s.payments[uuid] = "initiated"
	writeJSON(w, http.StatusCreated, map[string]string{
		"uuid": uuid, "provider": "payzen", "state": "initiated",
	})
}

func (s *scenarioServer) simulate(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	var body struct{ Outcome string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.payments[uuid]; !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	next := map[string]string{
		"PAID":       "captured",
		"AUTHORISED": "authorized",
		"UNPAID":     "declined",
		"EXPIRED":    "expired",
		"ABANDONED":  "declined",
	}[body.Outcome]
	if next == "" {
		http.Error(w, "unknown outcome", http.StatusBadRequest)
		return
	}
	s.payments[uuid] = next
	s.webhooks = append(s.webhooks, map[string]any{
		"id":        "wh-" + time.Now().UTC().Format("150405.000000000"),
		"status":    body.Outcome,
		"createdAt": time.Now().UTC(),
	})
	w.WriteHeader(http.StatusAccepted)
}

func (s *scenarioServer) get(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.payments[uuid]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"uuid": uuid, "state": st})
}

func (s *scenarioServer) list(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.webhooks)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// writeScenario écrit un YAML minimal dans un fichier temporaire et
// retourne son chemin. Encapsule la création du dossier temporaire
// pour ne pas polluer chaque test avec la même mécanique.
func writeScenario(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// env retourne une closure façon os.Getenv à partir d'une map, pour
// injecter l'environnement dans runCommand sans toucher os.Env.
func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestRunCommand_succes(t *testing.T) {
	t.Parallel()
	srv := newScenarioServer(t)
	path := writeScenario(t, `name: happy
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O-1
  - action: simulate
    status: captured
  - action: assert_state
    state: captured
`)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(),
		[]string{path},
		env(map[string]string{"PAYSIM_URL": srv.URL}),
		&stdout, &stderr)

	if code != exitOK {
		t.Fatalf("code = %d, veut %d\nstderr: %s", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK — happy") {
		t.Errorf("stdout = %q, veut contenir 'OK — happy'", stdout.String())
	}
}

func TestRunCommand_assertionEchouee(t *testing.T) {
	t.Parallel()
	srv := newScenarioServer(t)
	path := writeScenario(t, `name: wrong-state
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O
  - action: assert_state
    state: captured
`)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(),
		[]string{path},
		env(map[string]string{"PAYSIM_URL": srv.URL}),
		&stdout, &stderr)

	if code != exitAssertion {
		t.Fatalf("code = %d, veut %d\nstdout: %s", code, exitAssertion, stdout.String())
	}
	if !strings.Contains(stdout.String(), "ECHEC — wrong-state") {
		t.Errorf("stdout ne contient pas 'ECHEC — wrong-state': %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `obtenu "initiated"`) {
		t.Errorf("stdout ne contient pas le detail de l'assertion: %s", stdout.String())
	}
}

func TestRunCommand_erreurExecution(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		args   []string
		envMap map[string]string
		want   string // sous-chaîne attendue dans stderr
	}{
		{
			name:   "PAYSIM_URL absent",
			args:   []string{writeScenario(t, "name: x\nsteps: [{action: assert_state, state: initiated}]\n")},
			envMap: map[string]string{},
			want:   "PAYSIM_URL non defini",
		},
		{
			name:   "fichier manquant",
			args:   []string{filepath.Join(t.TempDir(), "does-not-exist.yml")},
			envMap: map[string]string{"PAYSIM_URL": "http://127.0.0.1:1"},
			want:   "ouverture",
		},
		{
			name: "yaml invalide",
			args: []string{writeScenario(t, "name: [not a string\n")},
			envMap: map[string]string{"PAYSIM_URL": "http://127.0.0.1:1"},
			want:   "decodage yaml",
		},
		{
			name:   "usage sans fichier",
			args:   []string{},
			envMap: map[string]string{"PAYSIM_URL": "http://127.0.0.1:1"},
			want:   "usage: paysim run",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := runCommand(context.Background(), c.args, env(c.envMap), &stdout, &stderr)
			if code != exitExec {
				t.Fatalf("code = %d, veut %d\nstderr: %s", code, exitExec, stderr.String())
			}
			if !strings.Contains(stderr.String(), c.want) {
				t.Errorf("stderr ne contient pas %q\nobtenu: %s", c.want, stderr.String())
			}
		})
	}
}

func TestRunCommand_httpDownRetourneExitExec(t *testing.T) {
	t.Parallel()
	// Serveur fermé immédiatement : les appels HTTP échouent avec
	// connection refused, l'action `create_payment` remonte une erreur
	// réseau qui n'est PAS une ErrAssertion → exitExec.
	srv := httptest.NewServer(http.NewServeMux())
	srv.Close()

	path := writeScenario(t, `name: no-server
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O
`)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(),
		[]string{path},
		env(map[string]string{"PAYSIM_URL": srv.URL}),
		&stdout, &stderr)

	if code != exitExec {
		t.Fatalf("code = %d, veut %d\nstdout: %s\nstderr: %s",
			code, exitExec, stdout.String(), stderr.String())
	}
}

func TestRunCommand_verbose(t *testing.T) {
	t.Parallel()
	srv := newScenarioServer(t)
	path := writeScenario(t, `name: trace
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O
  - action: simulate
    status: captured
  - action: assert_state
    state: captured
`)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(),
		[]string{"--verbose", path},
		env(map[string]string{"PAYSIM_URL": srv.URL}),
		&stdout, &stderr)

	if code != exitOK {
		t.Fatalf("code = %d, veut %d", code, exitOK)
	}
	// En verbose, chaque étape doit apparaître préfixée par « etape ».
	got := stdout.String()
	for _, want := range []string{"etape 1", "etape 2", "etape 3", "create_payment", "simulate", "assert_state"} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout ne contient pas %q\nobtenu: %s", want, got)
		}
	}
}

func TestRunCommand_tokenPropage(t *testing.T) {
	t.Parallel()
	// Serveur qui vérifie l'en-tête Authorization. Un token requis mais
	// absent doit provoquer une erreur d'exécution (401).
	mux := http.NewServeMux()
	mux.HandleFunc("POST /paysim/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{
			"uuid": "u1", "provider": "payzen", "state": "initiated",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	path := writeScenario(t, `name: auth
steps:
  - action: create_payment
    provider: payzen
    amount: 100
    currency: EUR
    order_id: O
`)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(),
		[]string{path},
		env(map[string]string{
			"PAYSIM_URL":       srv.URL,
			"PAYSIM_API_TOKEN": "secret",
		}),
		&stdout, &stderr)

	if code != exitOK {
		t.Fatalf("token non transmis correctement: code=%d stdout=%s stderr=%s",
			code, stdout.String(), stderr.String())
	}
}
