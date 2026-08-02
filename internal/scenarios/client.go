// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client parle à l'API de contrôle d'un Paysim distant. Wrapper minimal
// autour de net/http, spécialisé au sous-ensemble d'endpoints dont
// l'exécuteur de scénario a besoin.
//
// Volontairement typé au strict nécessaire — pas de miroir exhaustif des
// DTOs internes. Un champ ajouté côté API (ex. nouveau lien de nav) ne
// doit ni forcer une mise à jour du runner, ni casser sa désérialisation.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient construit un Client pointé sur baseURL (ex. `http://paysim:8080`
// ou `http://localhost:8080`). Le slash final est toléré. Token peut être
// vide (mode local ouvert). Timeout HTTP par défaut à 30s — un scénario
// suffisamment long doit utiliser wait plutôt qu'augmenter ce timeout.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetHTTPClient permet d'injecter un client HTTP maison, utile pour les
// tests (transport monté sur httptest.Server) ou pour un timeout ajusté.
func (c *Client) SetHTTPClient(hc *http.Client) { c.httpClient = hc }

// createPaymentReq est le miroir de api.CreatePaymentInput. Redéfini
// localement pour ne pas importer internal/api (dépendance de sens
// contraire à ce qu'on veut : le runner consomme l'API par HTTP, pas
// par appel Go direct).
type createPaymentReq struct {
	Provider string `json:"provider,omitempty"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	OrderID  string `json:"orderId"`
}

// CreatedPayment est la vue minimale d'un paiement fraîchement créé.
type CreatedPayment struct {
	UUID     string `json:"uuid"`
	Provider string `json:"provider"`
	State    string `json:"state"`
}

// CreatePayment appelle POST /paysim/api/v1/payments (endpoint générique).
func (c *Client) CreatePayment(ctx context.Context, in *CreatePayment) (*CreatedPayment, error) {
	body := createPaymentReq{
		Provider: in.Provider,
		Amount:   in.Amount,
		Currency: in.Currency,
		OrderID:  in.OrderID,
	}
	var out CreatedPayment
	if err := c.do(ctx, http.MethodPost, "/paysim/api/v1/payments", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// simulateReq est le miroir de api.SimulatePaymentRequest. Le vocabulaire
// est celui de PayZen (PAID/AUTHORISED/UNPAID/EXPIRED/ABANDONED) — le
// mapping depuis le vocabulaire domain du scénario est fait dans le runner.
type simulateReq struct {
	Outcome string `json:"outcome"`
	Channel string `json:"channel,omitempty"`
}

// SimulatePayment appelle POST /paysim/api/v1/payments/{uuid}/simulate.
// L'outcome PayZen déjà résolu par le runner. Channel vide = ipn côté
// runner, choix par défaut pour un scénario CI (pas de navigateur).
func (c *Client) SimulatePayment(ctx context.Context, uuid, outcome, channel string) error {
	body := simulateReq{Outcome: outcome, Channel: channel}
	return c.do(ctx, http.MethodPost, "/paysim/api/v1/payments/"+uuid+"/simulate", body, nil)
}

// PaymentDetail est la vue minimale du détail d'un paiement pour les
// besoins des assertions. Le journal d'événements est ignoré — le scénario
// n'assert que l'état pour l'instant.
type PaymentDetail struct {
	UUID  string `json:"uuid"`
	State string `json:"state"`
}

// GetPayment appelle GET /paysim/api/v1/payments/{uuid}.
func (c *Client) GetPayment(ctx context.Context, uuid string) (*PaymentDetail, error) {
	var out PaymentDetail
	if err := c.do(ctx, http.MethodGet, "/paysim/api/v1/payments/"+uuid, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WebhookEntry est la vue minimale d'un webhook livré, alignée sur
// api.WebhookEntry pour les champs consommés par assert_webhook.
type WebhookEntry struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListWebhooks appelle GET /paysim/api/v1/webhooks. Retourne l'ensemble
// des webhooks connus du serveur ; le filtrage par timestamp et par status
// est fait par le runner.
func (c *Client) ListWebhooks(ctx context.Context) ([]WebhookEntry, error) {
	var out []WebhookEntry
	if err := c.do(ctx, http.MethodGet, "/paysim/api/v1/webhooks", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// HTTPError encapsule une réponse non-2xx : status HTTP et corps brut.
// Les erreurs métier des endpoints Paysim (400 sur outcome inconnu, 404
// sur uuid inexistant) transitent par ce type — le runner peut ainsi
// distinguer un défaut du scénario d'un défaut réseau.
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

// Error implémente error.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// do exécute une requête typée : encode in en JSON, ajoute Bearer si
// configuré, décode dans out si non nil. Une réponse 4xx/5xx retourne
// un *HTTPError avec le corps brut pour diagnostic.
func (c *Client) do(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encodage %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("construction requete %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("appel %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return &HTTPError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(b)),
		}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decodage reponse %s %s: %w", method, path, err)
	}
	return nil
}

// ErrHTTP identifie une erreur HTTP produite par le client. Utile aux
// tests et au runner pour distinguer les erreurs réseau (dial refused,
// context deadline) des erreurs applicatives (400, 404, 500).
var ErrHTTP = errors.New("erreur http paysim")

// Unwrap fait pointer errors.Is(err, ErrHTTP) vers ErrHTTP pour tout
// *HTTPError — les appelants n'ont pas à connaître le type concret.
func (e *HTTPError) Unwrap() error { return ErrHTTP }
