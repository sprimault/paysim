// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewRejectsInvalidUpstream(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
	}{
		{"schema absent", "api.payzen.eu"},
		{"schema ftp", "ftp://api.payzen.eu"},
		{"host vide", "http://"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(c.url, t.TempDir(), discardLogger()); err == nil {
				t.Errorf("New(%q) accepté, attendu erreur", c.url)
			}
		})
	}
}

func TestNewCreatesOutputDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nouveau", "sous", "dossier")
	if _, err := New("http://example.com", dir, discardLogger()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("outputDir non créé : %v", err)
	}
}

func TestProxyRelaysRequestAndResponse(t *testing.T) {
	t.Parallel()
	// Serveur upstream de test qui capture ce qu'il reçoit et répond
	// avec un JSON reconnaissable.
	var upstreamReceived struct {
		method string
		path   string
		body   []byte
		auth   string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamReceived.method = r.Method
		upstreamReceived.path = r.URL.Path
		upstreamReceived.body, _ = io.ReadAll(r.Body)
		upstreamReceived.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"SUCCESS","upstream":true}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, err := New(upstream.URL, dir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(rec.Handler())
	defer proxy.Close()

	// Envoi de la requête au proxy.
	req, _ := http.NewRequest(http.MethodPost,
		proxy.URL+"/api-payment/V4/Charge/CreatePayment",
		strings.NewReader(`{"orderId":"test","amount":100}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("user", "pass")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	// Vérifier ce qui a été relayé à l'upstream.
	if upstreamReceived.method != "POST" {
		t.Errorf("méthode relayée = %q", upstreamReceived.method)
	}
	if upstreamReceived.path != "/api-payment/V4/Charge/CreatePayment" {
		t.Errorf("path relayé = %q", upstreamReceived.path)
	}
	if string(upstreamReceived.body) != `{"orderId":"test","amount":100}` {
		t.Errorf("body relayé = %q", string(upstreamReceived.body))
	}
	if !strings.HasPrefix(upstreamReceived.auth, "Basic ") {
		t.Errorf("Authorization non relayée : %q", upstreamReceived.auth)
	}

	// Vérifier la réponse renvoyée au client.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status client = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"upstream":true`) {
		t.Errorf("body client = %q, attendu contient upstream:true", string(body))
	}
}

func TestProxyWritesCapture(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "valeur-serveur")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, err := New(upstream.URL, dir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(rec.Handler())
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodPost,
		proxy.URL+"/api-payment/V4/Charge/CreatePayment",
		strings.NewReader(`{"amount":1500}`))
	req.Header.Set("X-Test", "valeur-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// Le dossier doit contenir un fichier .http.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("attendu 1 fichier dans %s, reçu %d", dir, len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".http") {
		t.Errorf("extension = %q, attendu .http", entries[0].Name())
	}
	if !strings.Contains(entries[0].Name(), "api-payment-v4-charge-createpayment") {
		t.Errorf("nom = %q, doit contenir le slug", entries[0].Name())
	}

	content, err := os.ReadFile(filepath.Join(dir, entries[0].Name())) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	txt := string(content)

	// Sections obligatoires.
	if !strings.Contains(txt, "--- REQUEST ---") {
		t.Error("section REQUEST absente")
	}
	if !strings.Contains(txt, "--- RESPONSE ---") {
		t.Error("section RESPONSE absente")
	}
	// Ligne de requête.
	if !strings.Contains(txt, "POST /api-payment/V4/Charge/CreatePayment") {
		t.Error("start-line request absente")
	}
	// Headers request.
	if !strings.Contains(txt, "X-Test: valeur-test") {
		t.Error("header X-Test absent")
	}
	// Body request.
	if !strings.Contains(txt, `{"amount":1500}`) {
		t.Error("body request absent")
	}
	// Start-line response.
	if !strings.Contains(txt, "201 Created") {
		t.Error("status response absent")
	}
	// Header response.
	if !strings.Contains(txt, "X-Custom: valeur-serveur") {
		t.Error("header X-Custom absent")
	}
	// Body response.
	if !strings.Contains(txt, `{"created":true}`) {
		t.Error("body response absent")
	}
}

func TestProxyPropagatesUpstreamFailure(t *testing.T) {
	t.Parallel()
	// Upstream retourne 500 : proxy relaie tel quel (le marchand doit
	// voir la vraie erreur upstream).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	rec, _ := New(upstream.URL, t.TempDir(), discardLogger())
	proxy := httptest.NewServer(rec.Handler())
	defer proxy.Close()

	resp, err := http.Post(proxy.URL+"/x", "text/plain", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, veut 500 (upstream relayé)", resp.StatusCode)
	}
}

func TestProxyReturns502OnUpstreamUnreachable(t *testing.T) {
	t.Parallel()
	// Upstream inexistant : proxy retourne 502.
	rec, err := New("http://127.0.0.1:1", t.TempDir(), discardLogger()) // port refusé
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(rec.Handler())
	defer proxy.Close()

	resp, err := http.Post(proxy.URL+"/x", "text/plain", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, veut 502", resp.StatusCode)
	}
}

func TestSanitize(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/api-payment/V4/Charge/CreatePayment": "api-payment-v4-charge-createpayment",
		"":                                     "capture",
		"/":                                    "capture",
		"////":                                 "capture",
		"/A/B/C":                               "a-b-c",
		"/api/V4":                              "api-v4",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, veut %q", in, got, want)
		}
	}
}
