// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandlerServesIndexAtRoot(t *testing.T) {
	t.Parallel()
	h, err := Handler("")
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, veut text/html", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<div id=\"root\">") {
		t.Errorf("body ne contient pas <div id=\"root\"> — page SPA cassée")
	}
}

func TestHandlerInjectsBasePathIntoIndex(t *testing.T) {
	t.Parallel()
	h, err := Handler("/paysim")
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/paysim")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	if strings.Contains(s, placeholder) {
		t.Errorf("le placeholder %q n'a pas été remplacé", placeholder)
	}
	if !strings.Contains(s, `window.__PAYSIM_BASE_PATH__ = "/paysim"`) {
		t.Errorf("basePath /paysim non injecté ; body head :\n%s", firstLines(s, 20))
	}
}

func TestHandlerFallbackToIndexForClientRoute(t *testing.T) {
	t.Parallel()
	h, err := Handler("")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Route react-router qui n'existe pas comme fichier statique —
	// doit servir index.html avec 200, pas 404.
	resp, _ := http.Get(srv.URL + "/payments/abc-123")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, veut 200 (fallback SPA)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<div id=\"root\">") {
		t.Error("le fallback ne renvoie pas index.html")
	}
}

func TestHandlerServesAssets(t *testing.T) {
	t.Parallel()
	h, err := Handler("")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// L'index.html référence un asset hashé /assets/index-XXX.js —
	// on le récupère dynamiquement pour éviter un test fragile aux
	// hashes qui changent à chaque build.
	resp, _ := http.Get(srv.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	assetPath := extractFirstAsset(string(body))
	if assetPath == "" {
		t.Fatal("aucun chemin d'asset trouvé dans index.html")
	}

	resp2, err := http.Get(srv.URL + assetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("asset %s : status = %d, veut 200", assetPath, resp2.StatusCode)
	}
}

// Les assets doivent être référencés en absolu dans le HTML servi.
// Vite les écrit en relatif (base: './'), et un chemin relatif se
// résout contre l'URL courante : depuis une route à deux segments, le
// navigateur demandait /payments/assets/index-XXX.js et recevait 404,
// donc aucun script, donc une page blanche. Le test précédent ne le
// voyait pas parce qu'il normalisait lui-même ./assets/ en /assets/ —
// il réparait ce que le navigateur ne répare pas.
func TestHandlerAssetsAreAbsolute(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"/", "/subscriptions", "/payments/abc-123"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			h, err := Handler("")
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(h)
			defer srv.Close()

			resp, err := http.Get(srv.URL + route)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if strings.Contains(string(body), `"./`) {
				t.Errorf("référence relative résiduelle dans le HTML servi sur %s", route)
			}

			// L'asset est suivi tel qu'il apparaît, résolu comme le ferait
			// le navigateur depuis la route demandée.
			asset := extractFirstAssetRaw(string(body))
			if asset == "" {
				t.Fatal("aucun asset référencé dans index.html")
			}
			ref, err := url.Parse(srv.URL + route)
			if err != nil {
				t.Fatal(err)
			}
			cible, err := ref.Parse(asset)
			if err != nil {
				t.Fatal(err)
			}

			resp2, err := http.Get(cible.String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp2.Body.Close() }()
			if resp2.StatusCode != http.StatusOK {
				t.Errorf("%s depuis %s : status = %d, veut 200", asset, route, resp2.StatusCode)
			}
		})
	}
}

// Sous un sous-chemin d'ingress, les assets absolus doivent porter le
// préfixe : sans lui, on remplacerait une page blanche par une autre.
func TestHandlerAssetsAbsoluteUnderBasePath(t *testing.T) {
	t.Parallel()
	h, err := Handler("/paysim")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/paysim/payments/abc-123")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	asset := extractFirstAssetRaw(string(body))
	if !strings.HasPrefix(asset, "/paysim/") {
		t.Errorf("asset = %q, veut un chemin prefixe de /paysim/", asset)
	}

	resp2, err := http.Get(srv.URL + asset)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("%s : status = %d, veut 200", asset, resp2.StatusCode)
	}
}

func TestHandlerNoStoreCacheHeaderOnIndex(t *testing.T) {
	t.Parallel()
	h, _ := Handler("")
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/")
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, veut no-store", got)
	}
}

// extractFirstAsset parse l'index.html pour trouver le premier
// chemin /assets/... référencé. Simple lookup pour ne pas dépendre
// des hashes de build.
func extractFirstAsset(html string) string {
	// Vite génère des chemins relatifs (./assets/...) car on a
	// configuré base: './'. On les normalise en /assets/...
	for _, needle := range []string{`href="./assets/`, `src="./assets/`, `href="/assets/`, `src="/assets/`} {
		i := strings.Index(html, needle)
		if i < 0 {
			continue
		}
		start := i + len(needle)
		end := strings.IndexAny(html[start:], `"`)
		if end < 0 {
			continue
		}
		return "/assets/" + html[start:start+end]
	}
	return ""
}

// extractFirstAssetRaw retourne le premier chemin d'asset tel qu'il est
// écrit dans le HTML, sans normalisation. C'est la différence avec
// extractFirstAsset, et c'est ce qui permet de vérifier ce que le
// navigateur va réellement demander.
func extractFirstAssetRaw(html string) string {
	for _, needle := range []string{`src="`, `href="`} {
		reste := html
		for {
			i := strings.Index(reste, needle)
			if i < 0 {
				break
			}
			start := i + len(needle)
			end := strings.Index(reste[start:], `"`)
			if end < 0 {
				break
			}
			valeur := reste[start : start+end]
			if strings.Contains(valeur, "assets/") {
				return valeur
			}
			reste = reste[start+end:]
		}
	}
	return ""
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}