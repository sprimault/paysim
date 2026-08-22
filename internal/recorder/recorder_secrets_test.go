// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCaptureNeContientPasLesSecrets vérifie qu'aucune valeur d'en-tête
// d'authentification n'atteint le fichier de capture.
//
// Une capture se produit en plaçant le proxy devant la vraie sandbox du
// fournisseur, donc avec de vrais identifiants, et finit versionnée dans
// testdata/ sur un dépôt public.
func TestCaptureNeContientPasLesSecrets(t *testing.T) {
	t.Parallel()

	const (
		basic  = "Basic NTEyMzQ1Njc6dGVzdHBhc3N3b3JkXzE"
		cookie = "session=3f8a1c9e2b7d"
		jeton  = "Bearer eyJhbGciOiJIUzI1NiJ9.charge.utile"
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", cookie)
		w.Header().Set("X-Custom", "valeur-serveur")
		_, _ = w.Write([]byte(`{"status":"SUCCESS"}`))
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
	req.Header.Set("Authorization", basic)
	req.Header.Set("Proxy-Authorization", jeton)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("X-Test", "valeur-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("attendu 1 capture, reçu %d", len(entries))
	}
	content, err := os.ReadFile(filepath.Join(dir, entries[0].Name())) // #nosec G304 -- chemin controle par le test
	if err != nil {
		t.Fatal(err)
	}
	txt := string(content)

	for _, secret := range []string{basic, cookie, jeton, "NTEyMzQ1Njc6dGVzdHBhc3N3b3JkXzE"} {
		if strings.Contains(txt, secret) {
			t.Errorf("la capture contient un secret en clair : %q", secret)
		}
	}

	// Les noms d'en-tête restent : c'est leur présence qui documente le
	// protocole, et un vecteur perdrait son intérêt sans eux.
	for _, nom := range []string{"Authorization:", "Proxy-Authorization:", "Cookie:", "Set-Cookie:"} {
		if !strings.Contains(txt, nom) {
			t.Errorf("le nom d'en-tête %q doit rester dans la capture", nom)
		}
	}
	if strings.Count(txt, valeurMasquee) != 4 {
		t.Errorf("attendu 4 valeurs masquées, trouvé %d", strings.Count(txt, valeurMasquee))
	}

	// Ce qui n'est pas sensible n'est pas touché : une capture reste une
	// capture, et masquer au-delà du nécessaire la rendrait inutile.
	for _, garde := range []string{"valeur-test", "valeur-serveur", `{"amount":1500}`, `{"status":"SUCCESS"}`} {
		if !strings.Contains(txt, garde) {
			t.Errorf("la capture doit conserver %q", garde)
		}
	}
}
