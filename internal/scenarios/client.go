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
	Provider           string            `json:"provider,omitempty"`
	Amount             int64             `json:"amount"`
	Currency           string            `json:"currency"`
	OrderID            string            `json:"orderId"`
	FormAction         string            `json:"formAction,omitempty"`
	Customer           *customerReq      `json:"customer,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	NotificationURL    string            `json:"notificationUrl,omitempty"`
	Card               *Card             `json:"card,omitempty"`
	PaymentMethodToken string            `json:"paymentMethodToken,omitempty"`
}

// customerReq est le miroir de payzen.Customer côté request scenario.
// Les deux champs sont à la racine du bloc customer, comme dans le
// contrat PayZen (voir loader.Customer).
type customerReq struct {
	Email     string `json:"email,omitempty"`
	Reference string `json:"reference,omitempty"`
}

// CreatedPayment est la vue minimale d'un paiement fraîchement créé.
// PaymentMethodToken est renseigné dans deux cas :
//   - après un enrôlement (Card fournie),
//   - après un rejeu one-click (echo du token utilisé).
type CreatedPayment struct {
	UUID               string `json:"uuid"`
	Provider           string `json:"provider"`
	State              string `json:"state"`
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`
}

// CreatePayment appelle POST /paysim/api/v1/payments (endpoint générique).
// Propage tous les champs 4.4.5 (Card, FormAction, NotificationURL).
func (c *Client) CreatePayment(ctx context.Context, in *CreatePayment) (*CreatedPayment, error) {
	body := createPaymentReq{
		Provider:        in.Provider,
		Amount:          in.Amount,
		Currency:        in.Currency,
		OrderID:         in.OrderID,
		FormAction:      in.FormAction,
		Metadata:        in.Metadata,
		NotificationURL: in.NotificationURL,
		Card:            in.Card,
	}
	if in.Customer != nil {
		body.Customer = &customerReq{
			Email:     in.Customer.Email,
			Reference: in.Customer.Reference,
		}
	}
	var out CreatedPayment
	if err := c.do(ctx, http.MethodPost, "/paysim/api/v1/payments", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChargeToken déclenche un rejeu one-click via le même endpoint
// générique. C'est un create_payment dont le body porte le
// paymentMethodToken au lieu de la Card — Paysim reconnaît le mode
// rejeu et applique directement l'outcome (PAID ou UNPAID selon les
// conditions du moyen de paiement).
func (c *Client) ChargeToken(
	ctx context.Context,
	provider, token string,
	amount int64,
	currency, orderID, notificationURL string,
) (*CreatedPayment, error) {
	body := createPaymentReq{
		Provider:           provider,
		Amount:             amount,
		Currency:           currency,
		OrderID:            orderID,
		NotificationURL:    notificationURL,
		PaymentMethodToken: token,
	}
	var out CreatedPayment
	if err := c.do(ctx, http.MethodPost, "/paysim/api/v1/payments", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokePaymentMethod marque un moyen de paiement comme révoqué côté
// Paysim. Idempotent (204 sur token inconnu). Utile aux scénarios qui
// veulent tester le refus de rejeu après révocation manuelle.
func (c *Client) RevokePaymentMethod(ctx context.Context, token string) error {
	path := "/paysim/api/v1/payment-methods/" + token + "/revoke"
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

// createSubReq est le miroir de api.CreateSubscriptionInput.
type createSubReq struct {
	Provider           string            `json:"provider,omitempty"`
	PaymentMethodToken string            `json:"paymentMethodToken"`
	Amount             int64             `json:"amount"`
	Currency           string            `json:"currency"`
	OrderID            string            `json:"orderId,omitempty"`
	EffectDate         string            `json:"effectDate,omitempty"`
	Rrule              string            `json:"rrule,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// SubscriptionDetail est la vue minimale d'un abonnement retournée par
// l'API. Champs alignés sur api.SubscriptionOutput ; suffisamment pour
// les assertions et la mémorisation côté runner.
type SubscriptionDetail struct {
	ID                 string `json:"id"`
	Provider           string `json:"provider"`
	PaymentMethodToken string `json:"paymentMethodToken"`
	Cancelled          bool   `json:"cancelled"`
}

// CreatedBilling résume un renewal déclenché par TriggerBilling.
type CreatedBilling struct {
	SubscriptionID string `json:"subscriptionId"`
	PaymentUUID    string `json:"paymentUuid"`
	State          string `json:"state"`
}

// CreateSubscription appelle POST /paysim/api/v1/subscriptions.
// Provider vide → payzen par défaut côté serveur.
func (c *Client) CreateSubscription(
	ctx context.Context,
	provider, token string,
	amount int64,
	currency, orderID, effectDate, rrule string,
	metadata map[string]string,
) (*SubscriptionDetail, error) {
	body := createSubReq{
		Provider:           provider,
		PaymentMethodToken: token,
		Amount:             amount,
		Currency:           currency,
		OrderID:            orderID,
		EffectDate:         effectDate,
		Rrule:              rrule,
		Metadata:           metadata,
	}
	var out SubscriptionDetail
	if err := c.do(ctx, http.MethodPost, "/paysim/api/v1/subscriptions", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSubscription appelle GET /paysim/api/v1/subscriptions/{id}.
func (c *Client) GetSubscription(ctx context.Context, id string) (*SubscriptionDetail, error) {
	var out SubscriptionDetail
	if err := c.do(ctx, http.MethodGet, "/paysim/api/v1/subscriptions/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TriggerBilling appelle POST .../{id}/trigger-billing. Retourne
// l'uuid du paiement créé et son état (captured ou declined).
func (c *Client) TriggerBilling(ctx context.Context, id string) (*CreatedBilling, error) {
	var out CreatedBilling
	if err := c.do(ctx, http.MethodPost,
		"/paysim/api/v1/subscriptions/"+id+"/trigger-billing", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelSubscription appelle POST .../{id}/cancel. Idempotent.
func (c *Client) CancelSubscription(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost,
		"/paysim/api/v1/subscriptions/"+id+"/cancel", nil, nil)
}

// simulateReq est le miroir de api.SimulatePaymentRequest. Le vocabulaire
// est celui de PayZen (PAID/AUTHORISED/UNPAID/EXPIRED/ABANDONED) — le
// mapping depuis le vocabulaire domain du scénario est fait dans le runner.
type simulateReq struct {
	Outcome         string     `json:"outcome"`
	Channel         string     `json:"channel,omitempty"`
	NotificationURL string     `json:"notificationUrl,omitempty"`
	Chaos           ChaosOpts  `json:"chaos,omitempty"`
	DeliveryDelayMs int        `json:"deliveryDelayMs,omitempty"`
}

// ChaosOpts est le miroir de payzen.WebhookChaos — struct locale pour
// éviter d'importer internal/providers/payzen depuis scenarios. Un
// intégrateur qui compose un simulate manuellement l'utilise via
// SimulateOpts, un scénario YAML la remplit implicitement via l'action
// inject (4.4.2b).
type ChaosOpts struct {
	Duplicate          bool `json:"duplicate,omitempty"`
	BadSignature       bool `json:"badSignature,omitempty"`
	RaceBeforeResponse bool `json:"raceBeforeResponse,omitempty"`
}

// SimulateOpts regroupe les options additionnelles d'un simulate.
// Struct dédiée plutôt qu'une liste de paramètres qui grossit — évite
// une signature à sept args et laisse la place aux extensions futures
// sans casse binaire des consommateurs.
type SimulateOpts struct {
	NotificationURL string
	Chaos           ChaosOpts
	DeliveryDelayMs int
}

// SimulatePayment appelle POST /paysim/api/v1/payments/{uuid}/simulate.
// L'outcome PayZen est déjà résolu par le runner. Channel vide = ipn
// (choix par défaut pour un scénario CI). Opts porte le chaos et le
// delivery delay quand un scénario les active via inject.
func (c *Client) SimulatePayment(ctx context.Context, uuid, outcome, channel string, opts SimulateOpts) error {
	body := simulateReq{
		Outcome:         outcome,
		Channel:         channel,
		NotificationURL: opts.NotificationURL,
		Chaos:           opts.Chaos,
		DeliveryDelayMs: opts.DeliveryDelayMs,
	}
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
	Outcome   string    `json:"outcome"`
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
