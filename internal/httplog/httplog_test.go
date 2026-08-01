// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package httplog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLogger construit un slog.Logger qui écrit du JSON dans le
// buffer fourni — permet aux tests d'inspecter les lignes émises.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func TestMiddlewareLoguesTheRequest(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), captureLogger(&buf))

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	got := parseLog(t, buf.String())
	if got["msg"] != "http_request" {
		t.Errorf("msg = %v", got["msg"])
	}
	if got["method"] != "GET" {
		t.Errorf("method = %v", got["method"])
	}
	if got["path"] != "/foo" {
		t.Errorf("path = %v", got["path"])
	}
	if got["status"].(float64) != 200 {
		t.Errorf("status = %v", got["status"])
	}
	if got["remote"] != "127.0.0.1" {
		t.Errorf("remote = %v (attendu sans port)", got["remote"])
	}
}

func TestMiddlewareCaptureStatusExplicite(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), captureLogger(&buf))

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "1.2.3.4:9999"
	h.ServeHTTP(httptest.NewRecorder(), req)

	got := parseLog(t, buf.String())
	if got["status"].(float64) != 418 {
		t.Errorf("status = %v, veut 418", got["status"])
	}
}

func TestMiddlewareCaptureBytes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	body := "hello world"
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}), captureLogger(&buf))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "1.2.3.4:1"
	h.ServeHTTP(httptest.NewRecorder(), req)

	got := parseLog(t, buf.String())
	if got["bytes"].(float64) != float64(len(body)) {
		t.Errorf("bytes = %v, veut %d", got["bytes"], len(body))
	}
}

func TestMiddlewareLoggerNilPassThrough(t *testing.T) {
	t.Parallel()
	// Aucun log attendu, juste vérifier que le handler passe.
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), nil)

	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rw.Code != 200 || rw.Body.String() != "ok" {
		t.Errorf("code=%d body=%q", rw.Code, rw.Body.String())
	}
}

// TestFlusherPreserved garantit qu'un endpoint SSE (qui a besoin de
// http.Flusher) continue de fonctionner à travers le middleware —
// sinon la connexion bufferise et le client reset. C'est le seul
// piège subtil du wrapping de ResponseWriter.
func TestFlusherPreserved(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("le ResponseWriter wrappé n'implémente pas http.Flusher")
			return
		}
		_, _ = w.Write([]byte("chunk1"))
		f.Flush()
	}), captureLogger(&buf))

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.RemoteAddr = "1.2.3.4:1"
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestClientIPStripsPort(t *testing.T) {
	t.Parallel()
	cases := []struct {
		remoteAddr, want string
	}{
		{"127.0.0.1:54321", "127.0.0.1"},
		{"[::1]:8080", "[::1]"},
		{"no-port", "no-port"},
		{"", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = c.remoteAddr
		if got := clientIP(req); got != c.want {
			t.Errorf("clientIP(%q) = %q, veut %q", c.remoteAddr, got, c.want)
		}
	}
}

// parseLog extrait la (première) ligne JSON de la sortie slog et la
// désérialise en map — évite du regex fragile sur le format wire.
func parseLog(t *testing.T, raw string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(strings.SplitN(raw, "\n", 2)[0])
	if line == "" {
		t.Fatal("aucune ligne loguée")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("json invalide : %v — ligne = %q", err, line)
	}
	return m
}
