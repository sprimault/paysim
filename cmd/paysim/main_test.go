// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/providers/payzen"
	"github.com/sprimault/paysim/internal/store/inmem"
)

// newMemStore monte un Store adosse a des depots en memoire, comme le
// fait main pour PAYSIM_STORE=memory. Remplace le payzen.MemoryStore
// supprime : le mode memoire ne passe plus par une implementation
// distincte du contrat.
func newMemStore() payzen.Store {
	return payzen.NewRepoStore(
		inmem.NewPaymentsRepository(),
		inmem.NewSubscriptionsRepository(),
		inmem.NewPaymentMethodsRepository(),
	)
}

// buildTestServer construit un httptest.Server câblé exactement comme
// le vrai binaire (buildMux + composants réels), avec le basePath donné.
// Renvoie le serveur, le flag ready pour tester le shutdown, et une
// fonction de cleanup qui arrête la queue.
func buildTestServer(t *testing.T, basePath string) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store := newMemStore()
	queue := delivery.New(&http.Client{Timeout: 2 * time.Second}, logger, 100)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = queue.Run(ctx)
	}()

	handler := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{})
	var ready atomic.Bool
	ready.Store(true)

	server := httptest.NewServer(buildMux(handler.Routes(), nil, nil, basePath, &ready))
	t.Cleanup(func() {
		server.Close()
		cancel()
		wg.Wait()
	})
	return server, &ready
}

func TestHealthzAlwaysOK(t *testing.T) {
	t.Parallel()
	server, _ := buildTestServer(t, "")

	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, veut 200", resp.StatusCode)
	}
}

func TestReadyzReflectsFlag(t *testing.T) {
	t.Parallel()
	server, ready := buildTestServer(t, "")

	// Nominal : ready → 200.
	resp, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("readyz initial = %d, veut 200", resp.StatusCode)
	}

	// Simuler shutdown : ready = false → 503.
	ready.Store(false)
	resp, err = http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("readyz apres ready=false = %d, veut 503", resp.StatusCode)
	}
}

func TestHealthzAndReadyzOutsideBasePath(t *testing.T) {
	t.Parallel()
	// Avec BasePath, healthz et readyz doivent rester accessibles à la
	// racine — c'est un invariant kubelet.
	server, _ := buildTestServer(t, "/paysim")

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("%s : %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, veut 200", path, resp.StatusCode)
		}
	}
}

func TestPayzenHandlerRoutedThroughBasePath(t *testing.T) {
	t.Parallel()
	server, _ := buildTestServer(t, "/paysim")

	// Sous BasePath, l'API PayZen répond.
	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/paysim/api-payment/V4/Charge/CreatePayment", nil)
	req.SetBasicAuth("u", "p")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	// 400 attendu (body vide → JSON invalide), l'important est que la
	// route soit atteinte (pas 404).
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("route non trouvée sous BasePath — StripPrefix defaillant")
	}
}

func TestPayzenHandlerRoutedAtRootWithoutBasePath(t *testing.T) {
	t.Parallel()
	server, _ := buildTestServer(t, "")

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api-payment/V4/Charge/CreatePayment", nil)
	req.SetBasicAuth("u", "p")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("route non trouvée à la racine")
	}
}
