// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package api expose les endpoints REST + SSE qui alimentent
// l'interface web embarquée. Découplé du paquet PayZen dans le sens
// où il ne connaît QUE le store payzen et la queue delivery (sources
// de vérité) — pas la logique métier.
//
// Toutes les routes sont montées sous /paysim/api/v1/*. Protection
// Bearer optionnelle via PAYSIM_API_TOKEN (identique au reste des
// endpoints /paysim/*).
package api

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/providers/payzen"
)

// Handler regroupe les dépendances nécessaires pour servir les
// endpoints API et SSE. Instancié dans cmd/paysim/main.go.
type Handler struct {
	store     *payzen.Store
	queue     *delivery.Queue
	publisher *bus.Bus
	logger    *slog.Logger
	token     string // vide = API ouverte
}

// NewHandler retourne un http.Handler qui multiplexe les endpoints
// UI sous /paysim/api/v1/*, protégé par Bearer si token non vide.
func NewHandler(store *payzen.Store, queue *delivery.Queue, publisher *bus.Bus, logger *slog.Logger, token string) http.Handler {
	h := &Handler{store: store, queue: queue, publisher: publisher, logger: logger, token: token}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /paysim/api/v1/payments", h.listPayments)
	mux.HandleFunc("GET /paysim/api/v1/payments/{uuid}", h.getPayment)
	mux.HandleFunc("GET /paysim/api/v1/webhooks", h.listWebhooks)
	mux.HandleFunc("GET /paysim/api/v1/webhooks/{id}", h.getWebhook)
	mux.HandleFunc("GET /paysim/api/v1/events/stream", h.streamEvents)

	return withBearer(mux, token, logger)
}

// withBearer applique un contrôle Bearer si token != "". Cohérent
// avec /paysim/simulate/*. Comparaison en temps constant.
func withBearer(next http.Handler, token string, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		want := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Paysim"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			logger.Debug("api_bearer_denied", "path", r.URL.Path)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// -----------------------------------------------------------------------------
// DTOs — définis ici pour ne pas exposer les structs internes de payzen
// dans la surface JSON de l'API. Les renommages/refactors internes
// n'impactent pas le contrat UI.
// -----------------------------------------------------------------------------

// PaymentSummary est le résumé d'un paiement pour les listes.
type PaymentSummary struct {
	UUID      string    `json:"uuid"`
	OrderID   string    `json:"orderId"`
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PaymentDetail ajoute le journal d'événements.
type PaymentDetail struct {
	PaymentSummary
	Events []EventEntry `json:"events"`
}

// EventEntry est une entrée du journal d'événements du domaine.
type EventEntry struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Amount int64     `json:"amount,omitempty"`
	Note   string    `json:"note,omitempty"`
}

// WebhookEntry résume une tentative de livraison — pour la liste UI.
type WebhookEntry struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Status      string    `json:"status"`
	StatusCode  int       `json:"statusCode,omitempty"`
	ErrorMsg    string    `json:"errorMsg,omitempty"`
	Attempts    int       `json:"attempts"`
	CreatedAt   time.Time `json:"createdAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// WebhookDetail ajoute les headers et le body pour la vue
// requête/réponse côte à côte.
type WebhookDetail struct {
	WebhookEntry
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// -----------------------------------------------------------------------------
// Endpoints
// -----------------------------------------------------------------------------

func (h *Handler) listPayments(w http.ResponseWriter, _ *http.Request) {
	txs := h.store.AllTransactions()
	out := make([]PaymentSummary, 0, len(txs))
	for _, tx := range txs {
		out = append(out, toPaymentSummary(tx))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	tx := h.store.ByUUID(uuid)
	if tx == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toPaymentDetail(tx))
}

func (h *Handler) listWebhooks(w http.ResponseWriter, _ *http.Request) {
	records := h.queue.Recent(200)
	out := make([]WebhookEntry, 0, len(records))
	for _, r := range records {
		out = append(out, toWebhookEntry(r))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	records := h.queue.Recent(200)
	for _, rec := range records {
		if rec.Webhook.ID == id {
			writeJSON(w, http.StatusOK, toWebhookDetail(rec))
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// streamEvents ouvre un flux Server-Sent Events. Chaque événement du
// bus est sérialisé en JSON et envoyé au format standard
// "data: <json>\n\n". La connexion se ferme quand le client se
// déconnecte (via r.Context()) ou quand le serveur s'arrête.
func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request) {
	if h.publisher == nil {
		http.Error(w, "event bus not configured", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // désactive buffering nginx si présent

	events, unsub := h.publisher.Subscribe(32)
	defer unsub()

	// Envoi initial d'un commentaire SSE pour flusher les headers.
	if _, err := w.Write([]byte(": stream open\n\n")); err != nil {
		return
	}
	flusher.Flush()

	// Heartbeat périodique pour maintenir la connexion vive à travers
	// les proxies qui timeout les connexions idle (nginx = 60s par défaut).
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case evt, ok := <-events:
			if !ok {
				return // bus fermé
			}
			payload := map[string]any{
				"type": evt.Type,
				"at":   evt.At,
				"data": evt.Data,
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				h.logger.Warn("sse_marshal_failed", "err", err)
				continue
			}
			if _, err := w.Write([]byte("data: ")); err != nil {
				return
			}
			if _, err := w.Write(raw); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// -----------------------------------------------------------------------------
// Convertisseurs internes → DTOs API
// -----------------------------------------------------------------------------

func toPaymentSummary(tx *payzen.Transaction) PaymentSummary {
	return PaymentSummary{
		UUID:      tx.UUID,
		OrderID:   tx.OrderID,
		Amount:    int64(tx.Amount),
		Currency:  tx.Currency,
		State:     string(tx.Payment.State()),
		CreatedAt: tx.CreatedAt,
		UpdatedAt: tx.UpdatedAt,
	}
}

func toPaymentDetail(tx *payzen.Transaction) PaymentDetail {
	events := tx.Payment.Events()
	dto := PaymentDetail{
		PaymentSummary: toPaymentSummary(tx),
		Events:         make([]EventEntry, 0, len(events)),
	}
	for _, e := range events {
		dto.Events = append(dto.Events, EventEntry{
			At:     e.At,
			Kind:   string(e.Kind),
			Amount: int64(e.Amount),
			Note:   e.Note,
		})
	}
	return dto
}

func toWebhookEntry(r delivery.WebhookRecord) WebhookEntry {
	return WebhookEntry{
		ID:          r.Webhook.ID,
		URL:         r.Webhook.URL,
		Status:      r.Status,
		StatusCode:  r.StatusCode,
		ErrorMsg:    r.ErrorMsg,
		Attempts:    r.Webhook.Attempts,
		CreatedAt:   r.Webhook.CreatedAt,
		CompletedAt: r.CompletedAt,
	}
}

func toWebhookDetail(r delivery.WebhookRecord) WebhookDetail {
	return WebhookDetail{
		WebhookEntry: toWebhookEntry(r),
		Headers:      r.Webhook.Headers,
		Body:         string(r.Webhook.Body),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
