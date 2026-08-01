// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package webui embarque le bundle Vite du front (React SPA) dans le
// binaire Go et l'expose comme http.Handler.
//
// Le contrat SPA : toute route qui n'existe pas comme fichier statique
// (routes react-router côté client — /payments/:uuid, /webhooks/:id)
// retourne index.html avec statut 200. C'est le fallback que le
// serveur SPA doit toujours faire, sinon un reload sur une route
// profonde donne un 404.
//
// PAYSIM_BASE_PATH est injecté au moment du serve dans index.html en
// remplaçant le placeholder "__PAYSIM_BASE_PATH__" que Vite laisse
// intact au build (voir web/index.html). C'est ce qui permet au même
// binaire de fonctionner à /, /paysim, /outils/paysim… sans rebuild.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

// distFS contient tout le bundle Vite. Le tag "all:" inclut les
// fichiers commençant par un point ou un souligné (aucun dans notre
// cas actuellement, mais anticipation raisonnable).
//
//go:embed all:dist
var distFS embed.FS

// placeholder est le littéral JavaScript exact que le HTML porte,
// guillemets INCLUS. Cibler la valeur avec ses guillemets évite de
// détruire aussi le NOM de propriété `window.__PAYSIM_BASE_PATH__`
// (qui contient la même chaîne sans guillemets).
const placeholder = `"__PAYSIM_BASE_PATH__"`

// Handler retourne un http.Handler qui sert le SPA.
//
// basePath est un préfixe du type "/paysim" ou "" (racine). Le serveur
// s'attend à recevoir des chemins commençant par basePath — chaque
// requête retire ce préfixe avant résolution dans le FS embarqué.
//
// L'index.html est lu une seule fois au démarrage : le placeholder
// PAYSIM_BASE_PATH est remplacé par la valeur fournie, le résultat est
// mis en cache pour toute la vie du handler.
func Handler(basePath string) (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}

	indexRaw, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}
	// strconv.Quote produit une chaîne JavaScript sûre (escape des
	// guillemets, caractères de contrôle, etc.) — évite un
	// PAYSIM_BASE_PATH exotique de casser le parsing JS.
	indexBody := strings.ReplaceAll(string(indexRaw), placeholder, strconv.Quote(basePath))

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Retire le préfixe basePath du chemin — le FS embarqué ne le
		// connaît pas.
		path := strings.TrimPrefix(r.URL.Path, basePath)
		if path == "" || path == "/" {
			serveIndex(w, indexBody)
			return
		}

		// Fallback SPA : toute route qui ne pointe pas vers un fichier
		// réel devient index.html. La règle « pas de . dans le dernier
		// segment » suffit pour distinguer un asset (fichier avec
		// extension) d'une route client (/payments/:uuid).
		trimmed := strings.TrimPrefix(path, "/")
		if !strings.Contains(lastSegment(trimmed), ".") {
			serveIndex(w, indexBody)
			return
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = path
		fileServer.ServeHTTP(w, r2)
	}), nil
}

func serveIndex(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Le HTML doit toujours être frais : il porte le PAYSIM_BASE_PATH
	// et les hashes des chunks. Un client qui garde l'ancien index
	// référencerait des assets qui n'existent plus après un redéploiement.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

func lastSegment(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return path
	}
	return path[i+1:]
}