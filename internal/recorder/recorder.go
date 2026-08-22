// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package recorder place Paysim en proxy transparent devant une vraie
// sandbox PSP (api.payzen.eu par exemple). Chaque échange (requête
// entrante marchand + réponse upstream) est capturé dans un fichier
// .http lisible et versionnable, utilisable ensuite comme vecteur de
// test ou fixture.
//
// C'est ce qui répond au coût récurrent d'obtenir des vecteurs
// authentiques (invariant 4) : au lieu de demander à un tiers de
// capturer manuellement, l'intégrateur pointe son marchand sur
// paysim-record → toutes les requêtes/réponses sont archivées telles
// quelles, byte-pour-byte fidèles au vrai PSP.
package recorder

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Recorder porte l'état du proxy enregistreur : URL upstream, dossier
// de sortie, compteur monotone pour numéroter les captures.
type Recorder struct {
	upstream  *url.URL
	outputDir string
	client    *http.Client
	logger    *slog.Logger
	counter   atomic.Int64
}

// New instancie un Recorder. Le upstream doit être une URL absolue
// (http:// ou https://) — le path incoming est propagé tel quel vers
// upstream. Le outputDir est créé s'il n'existe pas.
func New(upstreamURL, outputDir string, logger *slog.Logger) (*Recorder, error) {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("upstream invalide: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("upstream doit être http ou https, reçu %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("upstream sans host: %q", upstreamURL)
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, fmt.Errorf("création outputDir: %w", err)
	}
	return &Recorder{
		upstream:  u,
		outputDir: outputDir,
		client:    &http.Client{Timeout: 30 * time.Second},
		logger:    logger,
	}, nil
}

// Handler retourne le http.Handler qui reçoit les requêtes marchand,
// les relaie vers upstream, capture l'échange complet et renvoie la
// réponse upstream au marchand. Transparent au marchand — un test qui
// pointe sur paysim-record croit parler à PayZen réel.
func (r *Recorder) Handler() http.Handler {
	return http.HandlerFunc(r.serve)
}

func (r *Recorder) serve(w http.ResponseWriter, req *http.Request) {
	// 1. Lire le body entrant — nécessaire à capturer ET à réémettre.
	reqBody, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "lecture body: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = req.Body.Close()

	// 2. Construire la requête upstream : même méthode, même path/query,
	// headers copiés (sauf Host qui est réécrit).
	upURL := *r.upstream
	upURL.Path = strings.TrimSuffix(upURL.Path, "/") + req.URL.Path
	upURL.RawQuery = req.URL.RawQuery

	upReq, err := http.NewRequestWithContext(req.Context(), req.Method,
		upURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "construction upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	for k, vv := range req.Header {
		if k == "Host" {
			continue
		}
		upReq.Header[k] = vv
	}
	upReq.Host = r.upstream.Host

	// 3. Envoyer, récupérer la réponse.
	// #nosec G107,G704 -- SSRF non applicable : hôte verrouillé sur
	// r.upstream configuré par l'opérateur, seul le path/query du
	// client est relayé. C'est le rôle même du proxy transparent.
	upResp, err := r.client.Do(upReq)
	if err != nil {
		r.logger.Error("upstream_request_failed", "err", err, "url", upURL.String())
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = upResp.Body.Close() }()

	respBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		http.Error(w, "lecture réponse upstream: "+err.Error(), http.StatusBadGateway)
		return
	}

	// 4. Capturer AVANT de renvoyer, pour ne pas dépendre du timing
	// client (une capture qui échoue ne doit pas empêcher la réponse).
	if err := r.capture(req, reqBody, upResp, respBody); err != nil {
		r.logger.Error("capture_failed", "err", err)
	}

	// 5. Renvoyer au marchand la réponse upstream, telle quelle.
	for k, vv := range upResp.Header {
		w.Header()[k] = vv
	}
	w.WriteHeader(upResp.StatusCode)
	_, _ = w.Write(respBody)
}

// capture écrit un fichier .http dans outputDir avec la requête et la
// réponse complètes (headers + body). Format lisible et grepable :
// deux sections délimitées par des marqueurs, contenu exact byte-pour-
// byte pour préserver l'utilité en test de signature.
func (r *Recorder) capture(req *http.Request, reqBody []byte, resp *http.Response, respBody []byte) error {
	n := r.counter.Add(1)
	// Nom : YYYYMMDD-HHMMSS-N-slug.http, ex "20260731-152233-1-charge-createpayment.http"
	slug := sanitize(req.URL.Path)
	name := fmt.Sprintf("%s-%03d-%s.http", time.Now().UTC().Format("20060102-150405"), n, slug)
	path := filepath.Join(r.outputDir, name)

	f, err := os.Create(path) // #nosec G304 -- path construit à partir de composants sanitisés
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if err := writeCapture(f, req, reqBody, resp, respBody); err != nil {
		return err
	}
	r.logger.Info("capture_written", "file", path, "method", req.Method, "path", req.URL.Path, "status", resp.StatusCode)
	return nil
}

// writeCapture est extrait pour la testabilité — même logique, cible
// io.Writer arbitraire.
func writeCapture(w io.Writer, req *http.Request, reqBody []byte, resp *http.Response, respBody []byte) error {
	if _, err := fmt.Fprintf(w, "--- REQUEST ---\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s %s %s\n", req.Method, req.URL.RequestURI(), req.Proto); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Host: %s\n", req.Host); err != nil {
		return err
	}
	if err := writeHeaders(w, req.Header); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := w.Write(reqBody); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n\n--- RESPONSE ---\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s %s\n", resp.Proto, resp.Status); err != nil {
		return err
	}
	if err := writeHeaders(w, resp.Header); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := w.Write(respBody); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return nil
}

func writeHeaders(w io.Writer, h http.Header) error {
	for k, vv := range h {
		for _, v := range vv {
			if enTetesSensibles[http.CanonicalHeaderKey(k)] {
				v = valeurMasquee
			}
			if _, err := fmt.Fprintf(w, "%s: %s\n", k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// enTetesSensibles liste les en-têtes dont la valeur ne doit jamais
// atteindre un fichier de capture. Une capture est produite en plaçant
// paysim-record devant la vraie sandbox du fournisseur, avec les
// identifiants du contributeur : elle finit dans testdata/ et part en
// pull request sur un dépôt public. L'identifiant PSP y serait publié
// en clair, et un Basic se décode d'un copier-coller.
//
// Le nom de l'en-tête reste écrit : c'est sa présence qui a une valeur
// documentaire, pas sa valeur. Rien de ce qui est masqué ici n'entre
// dans le calcul d'une signature — kr-hash porte sur le corps — donc
// aucun vecteur ne perd sa validité.
var enTetesSensibles = map[string]bool{
	"Authorization":       true,
	"Proxy-Authorization": true,
	"Cookie":              true,
	"Set-Cookie":          true,
}

// valeurMasquee remplace la valeur d'un en-tête sensible. Volontairement
// constante, et non format.Mask : Mask conserve la longueur d'origine,
// ce qui laisserait déduire la taille du secret sans rien apporter à la
// lecture de la capture.
const valeurMasquee = "<masque par paysim-record>"

// sanitize transforme un path HTTP en composant de nom de fichier sûr.
// Ex : "/api-payment/V4/Charge/CreatePayment" → "api-payment-v4-charge-createpayment".
// Minuscule, remplace non-alphanumeriques par "-", trime les tirets aux extrémités.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	// Collapse séquences de tirets.
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if out == "" {
		out = "capture"
	}
	return out
}
