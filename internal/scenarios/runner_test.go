// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer simule les endpoints de contrôle Paysim consommés par le
// runner. Suffisamment complet pour exécuter les scénarios nominaux
// (one-shot, enrôlement + rejeu one-click, subscriptions natives,
// révocation). La logique métier reproduite ici est intentionnellement
// plus étroite que celle du vrai serveur — on teste le runner, pas le PSP.
type fakeServer struct {
	mu       sync.Mutex
	payments map[string]string      // uuid -> state courant
	methods  map[string]*fakeMethod // token -> moyen de paiement
	subs     map[string]*fakeSub    // subscriptionId -> abonnement
	webhooks []WebhookEntry
	// hooks permettent aux tests de forcer une réponse anormale sur un
	// endpoint donné (nil = comportement nominal).
	failCreate func(w http.ResponseWriter) bool
	failGet    func(w http.ResponseWriter) bool
}

// fakeSub est la vue minimale d'un abonnement côté fake.
type fakeSub struct {
	ID        string
	Token     string
	Cancelled bool
}

// fakeMethod porte les infos minimales sur une carte enregistrée qui
// permettent au fake de reproduire les décisions du vrai serveur.
type fakeMethod struct {
	PAN         string
	ExpiryMonth int
	ExpiryYear  int
	Revoked     bool

	// Attributs restitués par GET /payment-methods/{token}, ce que
	// assert_payment_method vérifie.
	HolderName      string
	Country         string
	ProductCategory string
	IssuerName      string
}

// fakeBrandFromPAN reproduit payzen.BrandFromBIN sur les seuls préfixes
// utilisés par les tests — dupliqué plutôt qu'importé, comme
// fakeDeclinedPANs, pour garder le fake indépendant du provider.
func fakeBrandFromPAN(pan string) string {
	switch {
	case strings.HasPrefix(pan, "4"):
		return "VISA"
	case strings.HasPrefix(pan, "5"), strings.HasPrefix(pan, "2"):
		return "MASTERCARD"
	case strings.HasPrefix(pan, "34"), strings.HasPrefix(pan, "37"):
		return "AMEX"
	}
	return ""
}

// fakeMaskPAN reproduit payzen.maskPAN : 6 en clair, 4 en clair, le
// reste masqué.
func fakeMaskPAN(pan string) string {
	if len(pan) < 10 {
		return pan
	}
	return pan[:6] + strings.Repeat("X", len(pan)-10) + pan[len(pan)-4:]
}

// fakeDeclinedPANs miroir léger de chaos.declinedTestPANs — dupliqué
// pour éviter d'importer internal/chaos dans le test.
var fakeDeclinedPANs = map[string]bool{
	"4000000000000002": true,
	"5105105105105100": true,
	"2223000000000007": true,
	"378282000000008":  true,
}

func newFakeServer(t *testing.T) (*fakeServer, *httptest.Server) {
	t.Helper()
	fs := &fakeServer{
		payments: make(map[string]string),
		methods:  make(map[string]*fakeMethod),
		subs:     make(map[string]*fakeSub),
	}
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
	mux.HandleFunc("GET /paysim/api/v1/payment-methods/{token}", fs.getMethod)
	mux.HandleFunc("POST /paysim/api/v1/payment-methods/{token}/revoke", fs.revoke)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions", fs.createSub)
	mux.HandleFunc("GET /paysim/api/v1/subscriptions/{id}", fs.getSub)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions/{id}/trigger-billing", fs.triggerBilling)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions/{id}/cancel", fs.cancelSub)
	return mux
}

func (fs *fakeServer) createSub(w http.ResponseWriter, r *http.Request) {
	var body createSubReq
	_ = json.NewDecoder(r.Body).Decode(&body)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, ok := fs.methods[body.PaymentMethodToken]; !ok {
		http.Error(w, "payment method inconnu", http.StatusBadRequest)
		return
	}
	id := "sub-" + timestamp()
	fs.subs[id] = &fakeSub{ID: id, Token: body.PaymentMethodToken}
	writeJSONResp(w, http.StatusCreated, SubscriptionDetail{
		ID: id, Provider: "payzen", PaymentMethodToken: body.PaymentMethodToken,
	})
}

func (fs *fakeServer) getSub(w http.ResponseWriter, r *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	sub, ok := fs.subs[r.PathValue("id")]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSONResp(w, http.StatusOK, SubscriptionDetail{
		ID: sub.ID, Provider: "payzen", PaymentMethodToken: sub.Token,
		Cancelled: sub.Cancelled,
	})
}

func (fs *fakeServer) triggerBilling(w http.ResponseWriter, r *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	sub, ok := fs.subs[r.PathValue("id")]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if sub.Cancelled {
		http.Error(w, "subscription cancelled", http.StatusBadRequest)
		return
	}
	pm, ok := fs.methods[sub.Token]
	if !ok {
		http.Error(w, "payment method inconnu", http.StatusBadRequest)
		return
	}
	uuid := "uuid-" + timestamp()
	state := "captured"
	if pm.Revoked || fakeIsExpired(pm) || fakeDeclinedPANs[pm.PAN] {
		state = "declined"
	}
	fs.payments[uuid] = state
	writeJSONResp(w, http.StatusCreated, CreatedBilling{
		SubscriptionID: sub.ID, PaymentUUID: uuid, State: state,
	})
}

func (fs *fakeServer) cancelSub(w http.ResponseWriter, r *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if sub, ok := fs.subs[r.PathValue("id")]; ok {
		sub.Cancelled = true
	}
	// Idempotent : ID inconnu → 204 quand même.
	w.WriteHeader(http.StatusNoContent)
}

// create couvre les trois flows du vrai handler.Create : nominal (ni
// Card ni PaymentMethodToken), enrôlement (Card → génère token),
// rejeu one-click (PaymentMethodToken → applique outcome selon état
// du moyen).
func (fs *fakeServer) create(w http.ResponseWriter, r *http.Request) {
	if fs.failCreate != nil && fs.failCreate(w) {
		return
	}
	var body createPaymentReq
	_ = json.NewDecoder(r.Body).Decode(&body)

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Rejeu one-click par token.
	if body.PaymentMethodToken != "" {
		pm, ok := fs.methods[body.PaymentMethodToken]
		if !ok {
			http.Error(w, "moyen de paiement inconnu", http.StatusBadRequest)
			return
		}
		uuid := "uuid-" + timestamp()
		state := "captured"
		if pm.Revoked || fakeIsExpired(pm) || fakeDeclinedPANs[pm.PAN] {
			state = "declined"
		}
		fs.payments[uuid] = state
		fs.webhooks = append(fs.webhooks, WebhookEntry{
			ID: "wh-" + timestamp(), Status: "delivered",
			Outcome: outcomeFor(state), CreatedAt: time.Now().UTC(),
		})
		writeJSONResp(w, http.StatusCreated, CreatedPayment{
			UUID: uuid, Provider: "payzen", State: state,
			PaymentMethodToken: body.PaymentMethodToken,
		})
		return
	}

	// Nominal ou enrôlement — état initié dans tous les cas.
	uuid := "uuid-" + timestamp()
	fs.payments[uuid] = "initiated"
	resp := CreatedPayment{UUID: uuid, Provider: "payzen", State: "initiated"}

	if body.Card != nil {
		token := "pmt-" + timestamp()
		fs.methods[token] = &fakeMethod{
			PAN:             body.Card.PAN,
			ExpiryMonth:     body.Card.ExpiryMonth,
			ExpiryYear:      body.Card.ExpiryYear,
			HolderName:      body.Card.HolderName,
			Country:         body.Card.Country,
			ProductCategory: body.Card.ProductCategory,
			IssuerName:      body.Card.IssuerName,
		}
		resp.PaymentMethodToken = token
	}
	writeJSONResp(w, http.StatusCreated, resp)
}

// getMethod sert GET /payment-methods/{token}. Usable est dérivé à la
// lecture, comme côté serveur réel : un champ figé deviendrait faux au
// premier changement de mois.
func (fs *fakeServer) getMethod(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	pm := fs.methods[token]
	if pm == nil {
		http.Error(w, "moyen de paiement inconnu", http.StatusNotFound)
		return
	}
	usable, reason := true, ""
	switch {
	case pm.Revoked:
		usable, reason = false, "moyen de paiement revoque"
	case fakeIsExpired(pm):
		usable, reason = false, "moyen de paiement expire"
	case fakeDeclinedPANs[pm.PAN]:
		usable, reason = false, "carte de test refusee"
	}
	writeJSONResp(w, http.StatusOK, PaymentMethodDetail{
		Token:           token,
		Brand:           fakeBrandFromPAN(pm.PAN),
		PANMasked:       fakeMaskPAN(pm.PAN),
		HolderName:      pm.HolderName,
		Country:         pm.Country,
		ProductCategory: pm.ProductCategory,
		IssuerName:      pm.IssuerName,
		Usable:          usable,
		UnusableReason:  reason,
	})
}

// revoke marque le moyen comme révoqué. Idempotent sur token inconnu.
func (fs *fakeServer) revoke(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if pm := fs.methods[token]; pm != nil {
		pm.Revoked = true
	}
	w.WriteHeader(http.StatusNoContent)
}

// fakeIsExpired reproduit PaymentMethod.IsExpired : refuse si le
// mois/année d'expiration sont strictement antérieurs à maintenant.
func fakeIsExpired(pm *fakeMethod) bool {
	now := time.Now().UTC()
	year, month, _ := now.Date()
	if pm.ExpiryYear < year {
		return true
	}
	if pm.ExpiryYear == year && pm.ExpiryMonth < int(month) {
		return true
	}
	return false
}

// outcomeFor reflète le mapping state → status webhook côté vrai
// serveur (applyOutcome retour → KrTransaction.Status).
func outcomeFor(state string) string {
	switch state {
	case "captured":
		return "PAID"
	case "declined":
		return "UNPAID"
	default:
		return state
	}
}

// simulate mappe l'outcome PayZen vers un état domain, met à jour le
// state du paiement et ajoute un webhook simulé au journal. L'état
// suivant reproduit le comportement de payzen.applyOutcome vu du runner.
func (fs *fakeServer) simulate(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	// Structure locale — miroir du body simulateReq côté client, on
	// n'extrait que ce dont le fake a besoin (outcome + chaos.duplicate).
	var req struct {
		Outcome string
		Chaos   struct {
			Duplicate bool `json:"duplicate"`
		} `json:"chaos"`
	}
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
		Status:    "delivered",
		Outcome:   req.Outcome,
		CreatedAt: time.Now().UTC(),
	})
	// Chaos duplicate : le vrai serveur enqueue le webhook deux fois.
	// Le fake reproduit ce comportement pour valider bout-en-bout que
	// le runner propage bien l'option jusqu'à l'API simulate.
	if req.Chaos.Duplicate {
		fs.webhooks = append(fs.webhooks, WebhookEntry{
			ID:        "wh-dup-" + timestamp(),
			Status:    "delivered",
			Outcome:   req.Outcome,
			CreatedAt: time.Now().UTC(),
		})
	}
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

	// Les cas d'échec portent un timeout court : l'assertion attend
	// désormais que le compte soit atteint, et un compte qui ne le sera
	// jamais consommerait sinon les 5 s du défaut.
	const bref = Duration(80 * time.Millisecond)

	cases := []struct {
		name    string
		want    int
		status  string
		outcome string
		timeout Duration
		wantErr string // sous-chaîne attendue, vide = pas d'erreur
	}{
		{"count exact sans filtre", 1, "", "", 0, ""},
		// Status porte sur l'acheminement, outcome sur le résultat
		// métier : les deux doivent filtrer indépendamment.
		{"filtre status livraison", 1, "delivered", "", 0, ""},
		{"filtre outcome metier", 1, "", "PAID", 0, ""},
		{"les deux filtres cumules", 1, "delivered", "PAID", 0, ""},
		{"count trop haut", 2, "", "", bref, "obtenu 1, veut 2"},
		{"status mauvais", 1, "failed", "", bref, "avec status=\"failed\": obtenu 0, veut 1"},
		{"outcome mauvais", 1, "", "UNPAID", bref, "avec outcome=\"UNPAID\": obtenu 0, veut 1"},
		{"outcome juste mais status faux", 1, "failed", "PAID", bref,
			"avec status=\"failed\" et outcome=\"PAID\": obtenu 0, veut 1"},
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
						Count: c.want, Status: c.status, Outcome: c.outcome, Timeout: c.timeout,
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

func TestRunner_injectModeInconnu(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "bad-mode",
		Steps: []Step{
			{Action: ActionInject, Inject: &Inject{Mode: "teleport"}},
		},
	}
	rep := r.Run(context.Background(), s)
	err := rep.Err()
	if err == nil || !strings.Contains(err.Error(), "inconnu") {
		t.Errorf("erreur = %v, veut contenir 'inconnu'", err)
	}
}

func TestRunner_injectDuplicateDoubleWebhook(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	// inject duplicate + simulate → 2 webhooks côté fake, assertion
	// vérifie la propagation du chaos jusqu'à l'endpoint simulate.
	s := &Scenario{
		Name: "chaos-duplicate",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "O",
			}},
			{Action: ActionInject, Inject: &Inject{Mode: "duplicate"}},
			{Action: ActionSimulate, Simulate: &Simulate{Status: "captured"}},
			{Action: ActionAssertWebhook, AssertWebhook: &AssertWebhook{Count: 2}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Errorf("scenario a echoue: %v", err)
	}
}

func TestRunner_injectPorteeUneEtape(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	// inject one-shot : le chaos est consommé par le 1er simulate.
	// Le 2e simulate n'a plus de chaos actif → 1 webhook seulement.
	s := &Scenario{
		Name: "chaos-one-shot",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "O1",
			}},
			{Action: ActionInject, Inject: &Inject{Mode: "duplicate"}},
			{Action: ActionSimulate, Simulate: &Simulate{Status: "captured"}}, // 2 webhooks
			// Nouvelle simulate sans réinjecter → 1 seul webhook.
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 2000, Currency: "EUR", OrderID: "O2",
			}},
			{Action: ActionSimulate, Simulate: &Simulate{Status: "captured"}}, // 1 webhook
			{Action: ActionAssertWebhook, AssertWebhook: &AssertWebhook{Count: 3}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Errorf("scenario a echoue: %v", err)
	}
}

func TestRunner_injectDelayInvalide(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "bad-delay",
		Steps: []Step{
			{Action: ActionInject, Inject: &Inject{Mode: "delay=abc"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err == nil || !strings.Contains(err.Error(), "delay invalide") {
		t.Errorf("erreur = %v, veut 'delay invalide'", err)
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

// --- Tests 4.4.5c : token pattern côté scénarios --------------------------

func TestRunner_enrolementCaptureToken(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	// create_payment avec card : Paysim renvoie un paymentMethodToken.
	// Le runner doit le mémoriser dans state.currentToken pour que
	// charge_token suivant puisse l'utiliser sans le nommer.
	s := &Scenario{
		Name: "enrol",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "SUB-1",
				Card: &Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
			}},
			{Action: ActionChargeToken, ChargeToken: &ChargeToken{
				Amount: 1000, Currency: "EUR", OrderID: "SUB-1-M2",
			}},
			{Action: ActionAssertState, AssertState: &AssertState{State: "captured"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Fatalf("scenario a echoue: %v", err)
	}
}

func TestRunner_chargeTokenSansTokenNiEnrolement(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "no-token",
		Steps: []Step{
			{Action: ActionChargeToken, ChargeToken: &ChargeToken{
				Amount: 1000, Currency: "EUR", OrderID: "X",
			}},
		},
	}
	rep := r.Run(context.Background(), s)
	err := rep.Err()
	if err == nil || !strings.Contains(err.Error(), "sans token") {
		t.Errorf("erreur = %v, veut contenir 'sans token'", err)
	}
}

func TestRunner_chargeTokenRefusMagicPAN(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "magic-refus",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "SUB",
				Card: &Card{PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2028},
			}},
			{Action: ActionChargeToken, ChargeToken: &ChargeToken{
				Amount: 1000, Currency: "EUR", OrderID: "SUB-M2",
			}},
			{Action: ActionAssertState, AssertState: &AssertState{State: "declined"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Errorf("scenario a echoue: %v", err)
	}
}

func TestRunner_chargeTokenRefusExpiration(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	// Carte expirée dans le passé : le rejeu doit remonter declined.
	s := &Scenario{
		Name: "exp-refus",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "SUB",
				Card: &Card{PAN: "4111111111111111", ExpiryMonth: 1, ExpiryYear: 2020},
			}},
			{Action: ActionChargeToken, ChargeToken: &ChargeToken{
				Amount: 1000, Currency: "EUR", OrderID: "SUB-M2",
			}},
			{Action: ActionAssertState, AssertState: &AssertState{State: "declined"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Errorf("scenario a echoue: %v", err)
	}
}

func TestRunner_chargeTokenTokenExplicite(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	c := NewClient(srv.URL, "")
	r := NewRunner(c)

	// Enrôlement direct via client pour obtenir un token à réutiliser.
	created, err := c.CreatePayment(context.Background(), &CreatePayment{
		Provider: "payzen", Amount: 1000, Currency: "EUR", OrderID: "PRE",
		Card: &Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
	})
	if err != nil || created.PaymentMethodToken == "" {
		t.Fatalf("prep create failed: %v / %+v", err, created)
	}

	// Un scénario qui utilise ce token explicitement, sans passer par
	// un create_payment (pas de currentToken automatique).
	s := &Scenario{
		Name: "token-explicite",
		Steps: []Step{
			{Action: ActionChargeToken, ChargeToken: &ChargeToken{
				Token:  created.PaymentMethodToken,
				Amount: 2500, Currency: "EUR", OrderID: "EXPLICIT",
			}},
			{Action: ActionAssertState, AssertState: &AssertState{State: "captured"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Errorf("scenario a echoue: %v", err)
	}
}

func TestClient_RevokePaymentMethod(t *testing.T) {
	t.Parallel()
	fs, srv := newFakeServer(t)
	c := NewClient(srv.URL, "")

	// Setup : un moyen de paiement en base côté fake.
	fs.methods["pmt-x"] = &fakeMethod{
		PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028,
	}

	if err := c.RevokePaymentMethod(context.Background(), "pmt-x"); err != nil {
		t.Fatalf("RevokePaymentMethod: %v", err)
	}
	if !fs.methods["pmt-x"].Revoked {
		t.Errorf("Revoked = false apres appel, veut true")
	}

	// Idempotent : token inconnu → 204, pas d'erreur.
	if err := c.RevokePaymentMethod(context.Background(), "unknown"); err != nil {
		t.Errorf("RevokePaymentMethod(inconnu) = %v, veut nil (idempotent)", err)
	}
}

// --- Tests 4.4.6b : subscriptions natives côté scénarios ------------------

func TestRunner_subscriptionNominal(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	// Enrôlement → create_subscription (utilise currentToken) →
	// trigger_billing (utilise currentSubID) → assert_state captured.
	s := &Scenario{
		Name: "sub-nominal",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 100, Currency: "EUR", OrderID: "INIT",
				Card: &Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
			}},
			{Action: ActionCreateSubscription, CreateSubscription: &CreateSubscription{
				Amount: 2990, Currency: "EUR", OrderID: "SUB",
				EffectDate: "2026-09-01", Rrule: "RRULE:FREQ=MONTHLY",
			}},
			{Action: ActionTriggerBilling, TriggerBilling: &TriggerBilling{}},
			{Action: ActionAssertState, AssertState: &AssertState{State: "captured"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Fatalf("scenario a echoue: %v", err)
	}
}

func TestRunner_subscriptionSansTokenPrealable(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "sub-orphan",
		Steps: []Step{
			{Action: ActionCreateSubscription, CreateSubscription: &CreateSubscription{
				Amount: 1000, Currency: "EUR", OrderID: "SUB",
			}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err == nil || !strings.Contains(err.Error(), "sans token") {
		t.Errorf("erreur = %v, veut contenir 'sans token'", err)
	}
}

func TestRunner_triggerBillingSansSub(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "trigger-orphan",
		Steps: []Step{
			{Action: ActionTriggerBilling, TriggerBilling: &TriggerBilling{}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err == nil || !strings.Contains(err.Error(), "sans subscription_id") {
		t.Errorf("erreur = %v, veut contenir 'sans subscription_id'", err)
	}
}

func TestRunner_triggerBillingRefusMagicPAN(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "sub-refus-magic",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 100, Currency: "EUR", OrderID: "INIT",
				Card: &Card{PAN: "4000000000000002", ExpiryMonth: 12, ExpiryYear: 2028},
			}},
			{Action: ActionCreateSubscription, CreateSubscription: &CreateSubscription{
				Amount: 1000, Currency: "EUR", OrderID: "SUB",
			}},
			{Action: ActionTriggerBilling, TriggerBilling: &TriggerBilling{}},
			{Action: ActionAssertState, AssertState: &AssertState{State: "declined"}},
		},
	}
	rep := r.Run(context.Background(), s)
	if err := rep.Err(); err != nil {
		t.Errorf("scenario a echoue: %v", err)
	}
}

func TestRunner_cancelSubscriptionPuisTriggerRefuse(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	// Cancel puis trigger doit remonter une erreur d'exécution
	// (le fake retourne 400).
	trueVal := true
	s := &Scenario{
		Name: "sub-cancel-then-trigger",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 100, Currency: "EUR", OrderID: "INIT",
				Card: &Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
			}},
			{Action: ActionCreateSubscription, CreateSubscription: &CreateSubscription{
				Amount: 1000, Currency: "EUR", OrderID: "SUB",
			}},
			{Action: ActionCancelSubscription, CancelSubscription: &CancelSubscription{}},
			{Action: ActionAssertSubscription, AssertSubscription: &AssertSubscription{Cancelled: &trueVal}},
			{Action: ActionTriggerBilling, TriggerBilling: &TriggerBilling{}},
		},
	}
	rep := r.Run(context.Background(), s)
	err := rep.Err()
	if err == nil {
		t.Fatalf("attendait erreur (trigger apres cancel)")
	}
	// Doit être une ErrHTTP (400 du fake), pas ErrAssertion.
	if !errors.Is(err, ErrHTTP) || errors.Is(err, ErrAssertion) {
		t.Errorf("veut erreur HTTP, obtenu: %v", err)
	}
}

func TestRunner_assertSubscriptionMismatch(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	// On assert cancelled=true alors qu'il vaut false → erreur assertion.
	trueVal := true
	s := &Scenario{
		Name: "sub-assert-mismatch",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 100, Currency: "EUR", OrderID: "INIT",
				Card: &Card{PAN: "4111111111111111", ExpiryMonth: 12, ExpiryYear: 2028},
			}},
			{Action: ActionCreateSubscription, CreateSubscription: &CreateSubscription{
				Amount: 1000, Currency: "EUR", OrderID: "SUB",
			}},
			{Action: ActionAssertSubscription, AssertSubscription: &AssertSubscription{Cancelled: &trueVal}},
		},
	}
	rep := r.Run(context.Background(), s)
	err := rep.Err()
	if err == nil {
		t.Fatalf("attendait erreur d'assertion")
	}
	if !errors.Is(err, ErrAssertion) {
		t.Errorf("veut ErrAssertion, obtenu: %v", err)
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("erreur = %v, veut mentionner 'cancelled'", err)
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

// TestRunner_assertWebhookAttendLaLivraison reproduit la course qui
// faisait echouer les scenarios en mode SQLite : la livraison n'est
// visible qu'apres coup, le worker historisant apres que le handler a
// repondu. L'assertion doit attendre au lieu de conclure au premier
// coup d'oeil.
func TestRunner_assertWebhookAttendLaLivraison(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	visible := false
	created := time.Now().UTC()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/paysim/api/v1/webhooks" {
			mu.Lock()
			defer mu.Unlock()
			if !visible {
				// Premiere lecture : le webhook n'est pas encore
				// historise. Il le devient pour les suivantes.
				visible = true
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = fmt.Fprintf(w, `[{"id":"wh-1","url":"http://m","status":"delivered","attempts":1,"createdAt":%q}]`,
				created.Format(time.RFC3339Nano))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	r := NewRunner(NewClient(srv.URL, ""))
	st := &state{startedAt: created.Add(-time.Second)}
	err := r.doAssertWebhook(context.Background(), st, &AssertWebhook{Count: 1})
	if err != nil {
		t.Fatalf("l'assertion doit attendre la livraison, obtenu: %v", err)
	}
}

func TestRunner_assertPaymentMethodNominal(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	vrai := true
	s := &Scenario{
		Name: "assert-pm",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "REG-1",
				FormAction: "REGISTER",
				Card: &Card{
					PAN: "5555555555554444", ExpiryMonth: 12, ExpiryYear: 2030,
					HolderName: "DUPONT JEAN", Country: "US",
					ProductCategory: "DEBIT", IssuerName: "BANQUE DE TEST",
				},
			}},
			{Action: ActionAssertPaymentMethod, AssertPaymentMethod: &AssertPaymentMethod{
				Brand: "MASTERCARD", PANMasked: "555555XXXXXX4444",
				HolderName: "DUPONT JEAN", Country: "US",
				ProductCategory: "DEBIT", IssuerName: "BANQUE DE TEST",
				Usable: &vrai,
			}},
		},
	}
	if err := r.Run(context.Background(), s).Err(); err != nil {
		t.Fatalf("scenario a echoue: %v", err)
	}
}

// Une marque erronée doit faire échouer l'assertion — c'est le défaut
// que l'action existe pour attraper.
func TestRunner_assertPaymentMethodMarqueFausse(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "brand-ko",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "REG-2",
				Card: &Card{PAN: "5555555555554444", ExpiryMonth: 12, ExpiryYear: 2030},
			}},
			{Action: ActionAssertPaymentMethod, AssertPaymentMethod: &AssertPaymentMethod{
				Brand: "VISA",
			}},
		},
	}
	err := r.Run(context.Background(), s).Err()
	if !errors.Is(err, ErrAssertion) {
		t.Fatalf("erreur = %v, veut ErrAssertion", err)
	}
	if !strings.Contains(err.Error(), "MASTERCARD") {
		t.Errorf("message = %q, veut citer la marque obtenue", err)
	}
}

// Plusieurs écarts sont rapportés d'un coup : quand un bloc entier n'est
// pas propagé, les découvrir un par un à chaque relance coûte cher.
func TestRunner_assertPaymentMethodCumuleLesEcarts(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "multi-ko",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "REG-3",
				Card: &Card{PAN: "5555555555554444", ExpiryMonth: 12, ExpiryYear: 2030},
			}},
			{Action: ActionAssertPaymentMethod, AssertPaymentMethod: &AssertPaymentMethod{
				HolderName: "DUPONT JEAN", Country: "US", IssuerName: "BANQUE DE TEST",
			}},
		},
	}
	err := r.Run(context.Background(), s).Err()
	if !errors.Is(err, ErrAssertion) {
		t.Fatalf("erreur = %v, veut ErrAssertion", err)
	}
	for _, attendu := range []string{"holder_name", "country", "issuer_name"} {
		if !strings.Contains(err.Error(), attendu) {
			t.Errorf("message = %q, veut citer %s", err, attendu)
		}
	}
}

// Un moyen inexploitable doit ressortir comme tel, avec son motif : le
// PAN de test refusé est le cas le plus stable — il ne dépend ni de la
// date courante ni d'un appel de révocation intercalé.
func TestRunner_assertPaymentMethodInexploitable(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	faux := false
	s := &Scenario{
		Name: "inexploitable",
		Steps: []Step{
			{Action: ActionCreatePayment, CreatePayment: &CreatePayment{
				Provider: "payzen", Amount: 0, Currency: "EUR", OrderID: "REG-4",
				Card: &Card{PAN: "5105105105105100", ExpiryMonth: 12, ExpiryYear: 2030},
			}},
			{Action: ActionAssertPaymentMethod, AssertPaymentMethod: &AssertPaymentMethod{
				Usable: &faux, UnusableReason: "carte de test refusee",
			}},
		},
	}
	if err := r.Run(context.Background(), s).Err(); err != nil {
		t.Fatalf("scenario a echoue: %v", err)
	}
}

func TestRunner_assertPaymentMethodSansToken(t *testing.T) {
	t.Parallel()
	_, srv := newFakeServer(t)
	r := NewRunner(NewClient(srv.URL, ""))

	s := &Scenario{
		Name: "sans-token",
		Steps: []Step{
			{Action: ActionAssertPaymentMethod, AssertPaymentMethod: &AssertPaymentMethod{
				Brand: "VISA",
			}},
		},
	}
	err := r.Run(context.Background(), s).Err()
	if err == nil || !strings.Contains(err.Error(), "sans token") {
		t.Errorf("erreur = %v, veut un message explicite sur l'absence de token", err)
	}
}
