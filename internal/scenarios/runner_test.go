// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer simule les endpoints de contrôle Paysim consommés par le
// runner. Suffisamment complet pour exécuter un scénario nominal et les
// cas d'erreur — pas plus. Un vrai test d'intégration bout-en-bout
// contre payzen.Handler + api.Handler viendra avec la CLI en 4.4.3.
type fakeServer struct {
	mu       sync.Mutex
	payments map[string]string // uuid -> state courant
	webhooks []WebhookEntry
	// hooks permettent aux tests de forcer une réponse anormale sur un
	// endpoint donné (nil = comportement nominal).
	failCreate func(w http.ResponseWriter) bool
	failGet    func(w http.ResponseWriter) bool
}

func newFakeServer(t *testing.T) (*fakeServer, *httptest.Server) {
	t.Helper()
	fs := &fakeServer{payments: make(map[string]string)}
	srv := httptest.NewServer(fs.router())
	t.Cleanup(srv.Close)
	return fs, srv
}

func (fs *fakeServer) router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /paysim/api/v1/payments", fs.create)
	mux.HandleFunc("POST /paysim/api/v1/payments/{uuid}/simulate", fs.simulate)
	mux.HandleFunc("GET /paysim/api/v1/payments/{uuid}", fs.get)
	mux.HandleFunc("GET /paysim/api/v1/webhooks", fs.list)
	return mux
}

func (fs *fakeServer) create(w http.ResponseWriter, r *http.Request) {
	if fs.failCreate != nil && fs.failCreate(w) {
		return
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	uuid := "uuid-" + timestamp()
	fs.payments[uuid] = "initiated"
	writeJSONResp(w, http.StatusCreated, CreatedPayment{
		UUID: uuid, Provider: "payzen", State: "initiated",
	})
}

// simulate mappe l'outcome PayZen vers un état domain, met à jour le
// state du paiement et ajoute un webhook simulé au journal. L'état
// suivant reproduit le comportement de payzen.applyOutcome vu du runner.
func (fs *fakeServer) simulate(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	var req struct{ Outcome string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, ok := fs.payments[uuid]; !ok {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}
	next, ok := outcomeToState[req.Outcome]
	if !ok {
		http.Error(w, "outcome inconnu", http.StatusBadRequest)
		return
	}
	fs.payments[uuid] = next
	fs.webhooks = append(fs.webhooks, WebhookEntry{
		ID:        "wh-" + timestamp(),
		Status:    req.Outcome,
		CreatedAt: time.Now().UTC(),
	})
	w.WriteHeader(http.StatusAccepted)
}

func (fs *fakeServer) get(w http.ResponseWriter, r *http.Request) {
	if fs.failGet != nil && fs.failGet(w) {
		return
	}
	uuid := r.PathValue("uuid")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	st, ok := fs.payments[uuid]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSONResp(w, http.StatusOK, PaymentDetail{UUID: uuid, State: st})
}

func (fs *fakeServer) list(w http.ResponseWriter, _ *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]WebhookEntry, len(fs.webhooks))
	copy(out, fs.webhooks)
	writeJSONResp(w, http.StatusOK, out)
}

// outcomeToState reflète le mapping serveur (applyOutcome dans payzen).
// Redéfini localement pour ne pas coupler le test au paquet payzen.
var outcomeToState = map[string]string{
	"PAID":       "captured",
	"AUTHORISED": "authorized",
	"UNPAID":     "declined",
	"EXPIRED":    "expired",
	"ABANDONED":  "declined",
}

// timestamp génère un identifiant lisible avec précision microseconde —
// suffisant pour distinguer des UUID/webhooks dans un test séquentiel.
func timestamp() string {
	return time.Now().UTC().Format("150405.000000000")
}

func writeJSONResp(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func TestRunner_scenarioNominal(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	c := NewClient(srv.URL, "")
	r := NewRunner(c)

	s, err := LoadFile(filepath.Join("testdata", "nominal.yml"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Fatalf("scenario a echoue: %v", err)
	}
	if len(rep.Steps) != len(s.Steps) {
		t.Fatalf("nb etapes = %d, veut %d", len(rep.Steps), len(s.Steps))
	}
	for _, st := range rep.Steps {
		if st.Err != nil {
			t.Errorf("etape %d (%s) en erreur: %v", st.Index, st.Action, st.Err)
		}
	}
	if rep.Duration() <= 0 {
		t.Errorf("Duration = %v, veut > 0", rep.Duration())
	}
}

func TestRunner_simulateSansCreate(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "orphan",
		Steps: []Step{
			{Action: ActionSimulate, Simulate: &Simulate{Status: "captured"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err == nil || !strings.Contains(err.Error(), "sans paiement courant") {
		t.Errorf("erreur = %v, veut contenir 'sans paiement courant'", err)
	}
}

func TestRunner_assertStateEchoue(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "mismatch",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "O-1",
			}},
			{Action: ActionAssertState, AssertState: &AssertState{State: "captured"}},
		},
	}
	rep := r.Run(context.Background(), s)
	err := rep.Err()
	if err == nil {
		t.Fatalf("attendait une erreur")
	}
	if !strings.Contains(err.Error(), `obtenu "initiated"`) {
		t.Errorf("erreur = %v, veut contenir 'obtenu \"initiated\"'", err)
	}
	// Classification : c'est une erreur d'assertion, pas d'exécution.
	// La CLI s'appuie sur ce marquage pour choisir son code retour.
	if !errors.Is(err, ErrAssertion) {
		t.Errorf("errors.Is(err, ErrAssertion) = false, veut true")
	}
	if errors.Is(err, ErrHTTP) {
		t.Errorf("errors.Is(err, ErrHTTP) = true, veut false")
	}
	// La 2e étape échoue ; la 1re a réussi.
	if rep.Steps[0].Err != nil {
		t.Errorf("etape 1 = %v, veut nil", rep.Steps[0].Err)
	}
	if rep.Steps[1].Err == nil {
		t.Errorf("etape 2 = nil, veut une erreur")
	}
}

func TestRunner_assertWebhookCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		want    int
		status  string
		wantErr string // sous-chaîne attendue, vide = pas d'erreur
	}{
		{"count exact sans filtre", 1, "", ""},
		{"count exact avec status", 1, "PAID", ""},
		{"count trop haut", 2, "", "obtenu 1, veut 2"},
		{"status mauvais", 1, "UNPAID", "avec status=\"UNPAID\": obtenu 0, veut 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// fakeServer dédié par sous-test : les webhooks s'accumulent
			// dans l'état partagé, un serveur commun aux 4 sous-tests
			// parallèles donnerait un compteur imprévisible.
			_, srv := newFakeServer(t)
			r := NewRunner(NewClient(srv.URL, ""))
			s := &Scenario{
				Name: "check-" + c.name,
				Steps: []Step{
					{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
						Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "O",
					}},
					{Action: ActionSimulate, Simulate: &Simulate{Status: "captured"}},
					{Action: ActionAssertWebhook, AssertWebhook: &AssertWebhook{
						Count: c.want, Status: c.status,
					}},
				},
			}
			rep := r.Run(context.Background(), s)
			err := rep.Err()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("attendait succes, obtenu: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("erreur = %v, veut contenir %q", err, c.wantErr)
			}
		})
	}
}

func TestRunner_injectNonSupporte(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "chaos",
		Steps: []Step{
			{Action: ActionInject, Inject: &Inject{Mode: "duplicate"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); !errors.Is(err, errInjectUnsupported) {
		t.Errorf("erreur = %v, veut errInjectUnsupported", err)
	}
}

func TestRunner_httpErrorRemonte(t *testing.T) {
	t.Parallel()
	fs, srv := newFakeServer(t)
	fs.failCreate = func(w http.ResponseWriter) bool {
		http.Error(w, "backend en panne", http.StatusServiceUnavailable)
		return true
	}
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "backend-down",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "O",
			}},
		},
	}
	rep := r.Run(context.Background(), s)
	err := rep.Err()
	if err == nil {
		t.Fatalf("attendait une erreur HTTP")
	}
	if !errors.Is(err, ErrHTTP) {
		t.Errorf("erreur = %v, veut errors.Is ErrHTTP", err)
	}
	if !strings.Contains(err.Error(), "backend en panne") {
		t.Errorf("erreur = %v, veut contenir 'backend en panne'", err)
	}
}

func TestRunner_ctxAnnule(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "long",
		Steps: []Step{
			{Action: ActionWait, Wait: &Wait{Duration: Duration(5 * time.Second)}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rep := r.Run(ctx, s)
	if err := rep.Err(); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("erreur = %v, veut context.DeadlineExceeded", err)
	}
}

func TestMapDomainToOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"captured", "PAID", false},
		{"authorized", "AUTHORISED", false},
		{"declined", "UNPAID", false},
		{"expired", "EXPIRED", false},
		{"abandoned", "ABANDONED", false},
		{"chargeback", "", true}, // pas de mapping — chargeback ne se simule pas via /simulate
		{"", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			got, err := mapDomainToOutcome(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got = %q, want %q", got, c.want)
			}
		})
	}
}
