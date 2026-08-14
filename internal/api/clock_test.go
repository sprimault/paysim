// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/clock"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/providers/payzen"
)

// setupHorloge monte une API avec une horloge pilotable et la rend, pour
// que le test puisse vérifier l'effet côté serveur et pas seulement la
// réponse HTTP.
func setupHorloge(t *testing.T) (*httptest.Server, *clock.Controllable) {
	t.Helper()
	logger := discardLogger()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, clock.System{}, 100)
	b := bus.New()
	queue.SetPublisher(b)
	clk := clock.NewControllable()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = queue.Run(ctx) }()

	server := httptest.NewServer(NewHandler(Deps{
		Store:     newMemStore(),
		Queue:     queue,
		Publisher: b,
		Logger:    logger,
		Clock:     clk,
	}))
	t.Cleanup(func() { server.Close(); cancel(); wg.Wait() })
	return server, clk
}

func lireEtat(t *testing.T, url string) ClockState {
	t.Helper()
	resp, err := http.Get(url + "/paysim/api/v1/clock")
	if err != nil {
		t.Fatalf("GET clock : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out ClockState
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func avancer(t *testing.T, url, corps string) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/paysim/api/v1/clock/advance",
		"application/json", bytes.NewBufferString(corps))
	if err != nil {
		t.Fatalf("POST advance : %v", err)
	}
	return resp
}

func TestClock_neutreAuDemarrage(t *testing.T) {
	t.Parallel()
	server, _ := setupHorloge(t)
	got := lireEtat(t, server.URL)
	if got.OffsetSeconds != 0 {
		t.Errorf("offsetSeconds = %v, veut 0 — la capacite doit etre inerte par defaut", got.OffsetSeconds)
	}
	if ecart := got.Now.Sub(time.Now().UTC()); ecart > time.Second || ecart < -time.Second {
		t.Errorf("now s'ecarte de %v de l'heure reelle", ecart)
	}
}

// TestClock_avanceEtCumul vérifie l'effet réel côté serveur, pas
// seulement le corps de la réponse : c'est l'horloge partagée avec le
// domaine qui doit bouger.
func TestClock_avanceEtCumul(t *testing.T) {
	t.Parallel()
	server, clk := setupHorloge(t)

	resp := avancer(t, server.URL, `{"duration":"48h"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", resp.StatusCode)
	}
	resp2 := avancer(t, server.URL, `{"duration":"24h"}`)
	defer func() { _ = resp2.Body.Close() }()

	if got := clk.Offset(); got != 72*time.Hour {
		t.Errorf("decalage cote serveur = %v, veut 72h", got)
	}
	if got := lireEtat(t, server.URL); got.Offset != "72h0m0s" {
		t.Errorf("offset = %q, veut 72h0m0s", got.Offset)
	}
}

func TestClock_reculRefuse(t *testing.T) {
	t.Parallel()
	server, clk := setupHorloge(t)
	resp := avancer(t, server.URL, `{"duration":"-1h"}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, veut 400", resp.StatusCode)
	}
	if got := clk.Offset(); got != 0 {
		t.Errorf("decalage = %v apres un refus, veut 0 — rien ne doit avoir bouge", got)
	}
}

func TestClock_dureeInvalide(t *testing.T) {
	t.Parallel()
	server, clk := setupHorloge(t)
	for _, corps := range []string{`{"duration":"demain"}`, `{"duration":""}`, `{}`} {
		resp := avancer(t, server.URL, corps)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("corps %s : status = %d, veut 400", corps, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	if got := clk.Offset(); got != 0 {
		t.Errorf("decalage = %v, veut 0", got)
	}
}

func TestClock_reset(t *testing.T) {
	t.Parallel()
	server, clk := setupHorloge(t)
	clk.Advance(96 * time.Hour)

	resp, err := http.Post(server.URL+"/paysim/api/v1/clock/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("POST reset : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", resp.StatusCode)
	}
	if got := clk.Offset(); got != 0 {
		t.Errorf("decalage apres reset = %v, veut 0", got)
	}
}

// TestClock_absente couvre le cas d'une instance construite sans
// horloge : une erreur explicite plutôt qu'un panic, et surtout plutôt
// qu'une route qui répondrait comme si elle avait avancé quelque chose.
func TestClock_absente(t *testing.T) {
	t.Parallel()
	server, _, _, _ := setup(t, "")
	for _, cas := range []struct{ methode, chemin string }{
		{http.MethodGet, "/paysim/api/v1/clock"},
		{http.MethodPost, "/paysim/api/v1/clock/advance"},
		{http.MethodPost, "/paysim/api/v1/clock/reset"},
	} {
		req, _ := http.NewRequest(cas.methode, server.URL+cas.chemin, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s : %v", cas.methode, cas.chemin, err)
		}
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s %s : status = %d, veut 500", cas.methode, cas.chemin, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestCreatePayment_marquesLyra couvre le point d'entrée du multi-marque
// côté API : les cinq marques sont acceptées et conservées, une valeur
// étrangère est refusée.
//
// C'est le contrat que voit un scénario ou l'interface — le protocole,
// lui, ne transporte pas de marque et prend celle de l'instance.
func TestCreatePayment_marquesLyra(t *testing.T) {
	t.Parallel()
	logger := discardLogger()
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, clock.System{}, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, clock.System{}, payzen.HandlerConfig{
		HMACKey: "k", RESTPassword: "p", Publisher: b,
	})
	server := httptest.NewServer(NewHandler(Deps{
		Store: store, Queue: queue, Publisher: b, Logger: logger, PayzenHandler: ph,
	}))
	t.Cleanup(server.Close)

	creer := func(provider string) int {
		t.Helper()
		corps := `{"amount":1000,"currency":"EUR","orderId":"CMD-` + provider + `"}`
		if provider != "" {
			corps = `{"provider":"` + provider + `","amount":1000,"currency":"EUR","orderId":"CMD-` + provider + `"}`
		}
		resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
			"application/json", bytes.NewBufferString(corps))
		if err != nil {
			t.Fatalf("POST %s : %v", provider, err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	for _, marque := range payzen.MarquesLyra {
		if got := creer(marque); got != http.StatusCreated && got != http.StatusOK {
			t.Errorf("provider %q : status = %d, veut une création", marque, got)
		}
	}
	if got := creer(""); got != http.StatusCreated && got != http.StatusOK {
		t.Errorf("provider vide : status = %d, veut une création", got)
	}
	// Une marque étrangère reste refusée : accepter n'importe quoi
	// laisserait un paiement invisible de tout adaptateur.
	if got := creer("monetico"); got != http.StatusBadRequest {
		t.Errorf("provider inconnu : status = %d, veut 400", got)
	}
}

// TestListPayments_filtreParMarque : le paramètre était accepté et
// ignoré, donc la liste rendait tout. Un appelant qui filtre sur une
// marque recevait les autres sans rien pour le lui dire.
func TestListPayments_filtreParMarque(t *testing.T) {
	t.Parallel()
	logger := discardLogger()
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, clock.System{}, 100)
	b := bus.New()
	ph := payzen.NewHandler(store, queue, logger, clock.System{}, payzen.HandlerConfig{
		HMACKey: "k", RESTPassword: "p", Publisher: b,
	})
	server := httptest.NewServer(NewHandler(Deps{
		Store: store, Queue: queue, Publisher: b, Logger: logger, PayzenHandler: ph,
	}))
	t.Cleanup(server.Close)

	for _, cas := range []struct{ marque, order string }{
		{"payzen", "CMD-PZ"}, {"systempay", "CMD-SP"}, {"scellius", "CMD-SC"},
	} {
		corps := `{"provider":"` + cas.marque + `","amount":1000,"currency":"EUR","orderId":"` + cas.order + `"}`
		resp, err := http.Post(server.URL+"/paysim/api/v1/payments",
			"application/json", bytes.NewBufferString(corps))
		if err != nil {
			t.Fatalf("POST %s : %v", cas.marque, err)
		}
		_ = resp.Body.Close()
	}

	lire := func(query string) []PaymentSummary {
		t.Helper()
		resp, err := http.Get(server.URL + "/paysim/api/v1/payments" + query)
		if err != nil {
			t.Fatalf("GET %s : %v", query, err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out []PaymentSummary
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	if got := lire(""); len(got) != 3 {
		t.Fatalf("sans filtre = %d paiements, veut 3", len(got))
	}
	for _, marque := range []string{"payzen", "systempay", "scellius"} {
		got := lire("?provider=" + marque)
		if len(got) != 1 {
			t.Errorf("filtre %s = %d paiements, veut 1", marque, len(got))
			continue
		}
		if got[0].Provider != marque {
			t.Errorf("filtre %s rend un paiement %s", marque, got[0].Provider)
		}
	}
	if got := lire("?provider=monetico"); len(got) != 0 {
		t.Errorf("filtre sur une marque absente = %d paiements, veut 0", len(got))
	}
}
