// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package httplog fournit un middleware d'access log HTTP structuré.
// Une ligne slog par requête reçue, sur le logger injecté : method,
// path, status, duration_ms, bytes, remote. Sortie standard uniquement,
// conforme au contrat de conteneur (CLAUDE.md : aucun log applicatif
// en fichier ; c'est le runtime — docker/kubectl — qui capture).
//
// Toutes les requêtes sont loguées en niveau Info, y compris les
// erreurs 4xx/5xx. En prod, un filtre côté runtime (grep sur "status":5
// ou export vers un backend structuré) suffit à isoler ce qui compte.
package httplog

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Middleware retourne un http.Handler qui délègue à `next` et logue
// chaque requête à sa fin sur `logger`. Le logger peut être nil — dans
// ce cas, aucun log n'est émis et le handler se comporte comme un
// passe-plat, utile en test.
func Middleware(next http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := wrap(w)
		next.ServeHTTP(rw, r)

		logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rw.bytes,
			"remote", clientIP(r),
		)
	})
}

// respWriter intercepte le status et le nombre d'octets écrits.
// Implémente http.Flusher pour ne pas casser les endpoints SSE
// (internal/api.streamEvents utilise Flush pour pousser chaque event).
type respWriter struct {
	http.ResponseWriter
	status  int
	bytes   int64
	wrote   bool
}

func wrap(w http.ResponseWriter) *respWriter {
	// Statut par défaut 200 : un handler qui appelle Write sans
	// WriteHeader implicite écrit 200. On garde la même sémantique.
	return &respWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *respWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *respWriter) Write(b []byte) (int, error) {
	w.wrote = true
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Flush relaie vers le ResponseWriter sous-jacent si celui-ci est un
// http.Flusher. Indispensable pour SSE — sans ça, api.streamEvents
// bufferise et le client voit ERR_CONNECTION_RESET.
func (w *respWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// clientIP extrait l'adresse sans port du RemoteAddr Go, qui est au
// format "host:port". Simple string split — pour un vrai reverse
// proxy behind, on ajouterait la lecture de X-Forwarded-For, mais
// c'est hors périmètre d'un simulateur qui tourne behind Ingress
// terminant le TLS.
func clientIP(r *http.Request) string {
	if i := strings.LastIndex(r.RemoteAddr, ":"); i >= 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}
