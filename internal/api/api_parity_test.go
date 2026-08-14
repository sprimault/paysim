// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/clock"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/providers/payzen"
	"github.com/sprimault/paysim/internal/store"
	"github.com/sprimault/paysim/internal/store/inmem"
	sqlitepkg "github.com/sprimault/paysim/internal/store/sqlite"
)

// Parite entre les deux backends de stockage.
//
// Le mode memoire est le defaut de l'image et ce que rencontre quiconque
// lance un `docker run` sans configuration. Il n'etait pourtant exerce
// nulle part : les scenarios de la CI tournent en SQLite, et le setup()
// des tests d'API monte un handler sans aucun depot — donc dans la
// configuration defectueuse elle-meme.
//
// Resultat : un enrolement rendait un token utilisable, mais
// /payment-methods repondait 200 avec une liste vide et le detail 404.
// L'API affirmait qu'un objet existant n'existait pas, ce qui est pire
// qu'une absence : un integrateur en conclut que son enrolement a
// echoue alors que tout fonctionne.
//
// Ce test fixe la regle : a sequence identique, les deux backends
// repondent identiquement. Il ne verifie pas une implementation, il
// verifie une equivalence — c'est ce qui manquait.

// parityEnv est un serveur monte comme main.go le fait : handler payzen
// cable et depots fournis. Volontairement distinct de setup(), qui
// laisse les depots nil et n'aurait donc rien pu detecter.
type parityEnv struct {
	server *httptest.Server
}

func newParityEnv(t *testing.T, backend string) *parityEnv {
	t.Helper()
	logger := discardLogger()

	var (
		payzenStore payzen.Store
		paymentRepo store.PaymentRepository
		subsRepo    store.SubscriptionRepository
		methodsRepo store.PaymentMethodRepository
	)

	switch backend {
	case "sqlite":
		db, err := sqlitepkg.Open(filepath.Join(t.TempDir(), "parity.db"))
		if err != nil {
			t.Fatalf("ouverture SQLite : %v", err)
		}
		pr, err := sqlitepkg.NewPaymentsRepository(db)
		if err != nil {
			t.Fatalf("repo payments : %v", err)
		}
		sr, err := sqlitepkg.NewSubscriptionsRepository(db)
		if err != nil {
			t.Fatalf("repo subscriptions : %v", err)
		}
		mr, err := sqlitepkg.NewPaymentMethodsRepository(db)
		if err != nil {
			t.Fatalf("repo payment methods : %v", err)
		}
		paymentRepo, subsRepo, methodsRepo = pr, sr, mr
		payzenStore = payzen.NewRepoStore(clock.System{}, pr, sr, mr)
		t.Cleanup(func() { _ = db.Close() })
	default:
		// Même montage que la branche mémoire de main.go : trois dépôts
		// et le même wrapper. Reproduire le câblage réel est le point du
		// test — le monter autrement reviendrait à vérifier une
		// configuration que personne n'exécute.
		paymentRepo = inmem.NewPaymentsRepository(0, nil)
		subsRepo = inmem.NewSubscriptionsRepository()
		methodsRepo = inmem.NewPaymentMethodsRepository()
		payzenStore = payzen.NewRepoStore(clock.System{}, paymentRepo, subsRepo, methodsRepo)
	}

	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, clock.System{}, 100)
	b := bus.New()
	queue.SetPublisher(b)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = queue.Run(ctx)
	}()

	ph := payzen.NewHandler(payzenStore, queue, logger, clock.System{}, payzen.HandlerConfig{
		HMACKey:   "k", RESTPassword: "pwd-rest",
		Publisher: b,
	})
	handler := NewHandler(Deps{
		Store:             payzenStore,
		PaymentRepo:       paymentRepo,
		SubscriptionRepo:  subsRepo,
		PaymentMethodRepo: methodsRepo,
		Queue:             queue,
		Publisher:         b,
		Logger:            logger,
		PayzenHandler:     ph,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		cancel()
		wg.Wait()
	})
	return &parityEnv{server: server}
}

func (e *parityEnv) post(t *testing.T, path, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(e.server.URL+"/paysim/api/v1"+path,
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s : %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *parityEnv) getCode(t *testing.T, path string) int {
	t.Helper()
	resp, err := http.Get(e.server.URL + "/paysim/api/v1" + path)
	if err != nil {
		t.Fatalf("GET %s : %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func (e *parityEnv) count(t *testing.T, path string) int {
	t.Helper()
	resp, err := http.Get(e.server.URL + "/paysim/api/v1" + path)
	if err != nil {
		t.Fatalf("GET %s : %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out []any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("GET %s : reponse non listable : %v", path, err)
	}
	return len(out)
}

// observation est ce qu'un integrateur voit apres la meme sequence.
// Les identifiants generes ne sont pas compares — ils different par
// construction — seulement ce qui est observable et doit concorder.
type observation struct {
	tokenRendu       bool
	methodsListe     int
	methodDetailCode int
	subsListe        int
	paymentsListe    int
	oneClickCode     int
}

func (e *parityEnv) run(t *testing.T) observation {
	t.Helper()
	var obs observation

	code, out := e.post(t, "/payments", `{
		"amount": 0, "currency": "EUR", "orderId": "PAR-REG",
		"formAction": "REGISTER",
		"card": {"pan": "5555555555554444", "expiryMonth": 12, "expiryYear": 2030}
	}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("REGISTER : code %d", code)
	}
	token, _ := out["paymentMethodToken"].(string)
	obs.tokenRendu = token != ""
	if token == "" {
		t.Fatal("REGISTER n'a pas rendu de paymentMethodToken")
	}

	obs.methodsListe = e.count(t, "/payment-methods")
	obs.methodDetailCode = e.getCode(t, "/payment-methods/"+token)

	if c, _ := e.post(t, "/subscriptions", `{
		"paymentMethodToken": "`+token+`",
		"amount": 2990, "currency": "EUR", "orderId": "PAR-SUB",
		"effectDate": "2026-09-01T00:00:00Z",
		"rrule": "RRULE:FREQ=MONTHLY;INTERVAL=1"
	}`); c != http.StatusCreated && c != http.StatusOK {
		t.Fatalf("creation d'abonnement : code %d", c)
	}
	obs.subsListe = e.count(t, "/subscriptions")

	obs.oneClickCode, _ = e.post(t, "/payments", `{
		"amount": 1990, "currency": "EUR", "orderId": "PAR-ONECLICK",
		"paymentMethodToken": "`+token+`"
	}`)
	obs.paymentsListe = e.count(t, "/payments")
	return obs
}

func TestParite_memoireEtSQLiteRepondentPareil(t *testing.T) {
	t.Parallel()

	memoire := newParityEnv(t, "memory").run(t)
	sqlite := newParityEnv(t, "sqlite").run(t)

	if memoire == sqlite {
		return
	}

	// Comparaison champ par champ : un diff de struct entier ne dit pas
	// lequel a diverge, et c'est justement ce qu'on veut lire.
	cas := []struct {
		nom            string
		mem, sql       any
		pourquoiCaCompte string
	}{
		{"token rendu par REGISTER", memoire.tokenRendu, sqlite.tokenRendu,
			"sans token, l'enrolement est inutilisable"},
		{"GET /payment-methods (entrees)", memoire.methodsListe, sqlite.methodsListe,
			"une liste vide fait croire qu'aucun alias n'existe"},
		{"GET /payment-methods/{token}", memoire.methodDetailCode, sqlite.methodDetailCode,
			"un 404 sur un token utilisable fait conclure a un echec d'enrolement"},
		{"GET /subscriptions (entrees)", memoire.subsListe, sqlite.subsListe,
			"un abonnement cree en 201 doit apparaitre"},
		{"GET /payments (entrees)", memoire.paymentsListe, sqlite.paymentsListe,
			"les paiements doivent se lister dans les deux modes"},
		{"paiement one-click", memoire.oneClickCode, sqlite.oneClickCode,
			"le token doit etre debitable des deux cotes"},
	}
	for _, c := range cas {
		if c.mem != c.sql {
			t.Errorf("%s : memoire=%v, sqlite=%v — %s",
				c.nom, c.mem, c.sql, c.pourquoiCaCompte)
		}
	}
}
