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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
	"github.com/sprimault/paysim/internal/providers/payzen"
	"github.com/sprimault/paysim/internal/store"
)

// Deps regroupe les dépendances de l'API UI. Struct plutôt que
// 6 paramètres positionnels — plus lisible et extensible sans
// breaking change côté cmd/paysim.
type Deps struct {
	Store         payzen.Store
	PaymentRepo   store.PaymentRepository // optionnel — nil en mode mémoire ; permet les endpoints DELETE cross-provider
	Queue         *delivery.Queue
	Publisher     *bus.Bus
	Logger        *slog.Logger
	Token         string // Bearer requis si non vide, sinon API ouverte
	PayzenHandler *payzen.Handler
}

// Handler regroupe les dépendances nécessaires pour servir les
// endpoints API et SSE. Instancié dans cmd/paysim/main.go.
type Handler struct {
	store         payzen.Store
	paymentRepo   store.PaymentRepository
	queue         *delivery.Queue
	publisher     *bus.Bus
	logger        *slog.Logger
	token         string
	payzenHandler *payzen.Handler
}

// NewHandler retourne un http.Handler qui multiplexe les endpoints
// UI sous /paysim/api/v1/*, protégé par Bearer si Token non vide.
func NewHandler(deps Deps) http.Handler {
	h := &Handler{
		store:         deps.Store,
		paymentRepo:   deps.PaymentRepo,
		queue:         deps.Queue,
		publisher:     deps.Publisher,
		logger:        deps.Logger,
		token:         deps.Token,
		payzenHandler: deps.PayzenHandler,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /paysim/api/v1/payments", h.listPayments)
	mux.HandleFunc("POST /paysim/api/v1/payments", h.createPayment)
	mux.HandleFunc("DELETE /paysim/api/v1/payments", h.deletePayments)
	mux.HandleFunc("GET /paysim/api/v1/payments/{uuid}", h.getPayment)
	mux.HandleFunc("DELETE /paysim/api/v1/payments/{uuid}", h.deletePayment)
	mux.HandleFunc("POST /paysim/api/v1/payments/{uuid}/simulate", h.simulatePayment)
	mux.HandleFunc("GET /paysim/api/v1/webhooks", h.listWebhooks)
	mux.HandleFunc("GET /paysim/api/v1/webhooks/{id}", h.getWebhook)
	mux.HandleFunc("POST /paysim/api/v1/webhooks/{id}/replay", h.replayWebhook)
	mux.HandleFunc("GET /paysim/api/v1/events/stream", h.streamEvents)

	return withBearer(mux, deps.Token, deps.Logger)
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
	Provider  string    `json:"provider"`
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

// CreatePaymentInput est le corps de POST /paysim/api/v1/payments,
// endpoint générique de création cross-provider. Le champ Provider
// choisit l'adaptateur qui matérialise le paiement (seul "payzen"
// est câblé aujourd'hui ; Stripe arrivera en phase 5). Vide = payzen
// par défaut pour ne pas alourdir les scénarios monoprovider.
type CreatePaymentInput struct {
	Provider string        `json:"provider,omitempty"`
	Amount   format.Amount `json:"amount"`
	Currency string        `json:"currency"`
	OrderID  string        `json:"orderId"`
}

// CreatePaymentOutput retourne l'uuid attribué au paiement — seul
// identifiant nécessaire pour cibler ensuite les endpoints
// /payments/{uuid}/*. State est renvoyé pour éviter au client un
// GET juste après pour lire l'état initial.
type CreatePaymentOutput struct {
	UUID     string `json:"uuid"`
	Provider string `json:"provider"`
	State    string `json:"state"`
}

// SimulatePaymentRequest est le corps de POST
// /paysim/api/v1/payments/{uuid}/simulate. L'UI n'a pas à connaître
// le formToken interne — Paysim le retrouve depuis l'uuid. Le champ
// channel choisit entre retour navigateur (défaut) et IPN pur.
type SimulatePaymentRequest struct {
	Outcome           string `json:"outcome"` // PAID | AUTHORISED | UNPAID | EXPIRED | ABANDONED
	Channel           string `json:"channel,omitempty"` // "browserReturn" (défaut) | "ipn"
	ReturnURL         string `json:"returnUrl,omitempty"`
	NotificationURL   string `json:"notificationUrl,omitempty"`
	CardBrand         string `json:"cardBrand,omitempty"`
	ThreeDSStatus     string `json:"threeDSStatus,omitempty"`
	ErrorCode         string `json:"errorCode,omitempty"`
	ErrorMessage      string `json:"errorMessage,omitempty"`
}

// SimulatePaymentResponse retourne le deliveryId et le hash calculé.
type SimulatePaymentResponse struct {
	DeliveryID string `json:"deliveryId"`
	KrHash     string `json:"krHash"`
	Channel    string `json:"channel"`
}

// ReplayWebhookResponse retourne l'identifiant du nouveau webhook
// enqueue. L'original reste dans l'historique — le rejeu est une
// nouvelle tentative distincte.
type ReplayWebhookResponse struct {
	NewDeliveryID string `json:"newDeliveryId"`
}

// -----------------------------------------------------------------------------
// Endpoints
// -----------------------------------------------------------------------------

// createPayment traite POST /paysim/api/v1/payments : endpoint générique
// de création cross-provider. Délègue à l'adaptateur du provider demandé
// après validation. Motivation : les scénarios de test et les intégrateurs
// qui veulent orchestrer un paiement sans dépendre du format natif d'un
// PSP. L'ajout de Stripe (phase 5) se fera par ajout d'un cas au switch.
func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var req CreatePaymentInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	provider := req.Provider
	if provider == "" {
		provider = "payzen"
	}
	switch provider {
	case "payzen":
		if h.payzenHandler == nil {
			http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
			return
		}
		tx, err := h.payzenHandler.Create(payzen.CreateInput{
			Amount:   req.Amount,
			Currency: req.Currency,
			OrderID:  req.OrderID,
		})
		if err != nil {
			// Les erreurs domain (montant, devise) sont fonctionnelles, on
			// retourne 400 ; toute autre (store, génération d'uuid) est
			// infra et remonte en 500 avec log.
			if isDomainErr(err) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			h.logger.Error("api_create_payment_failed", "provider", provider, "err", err)
			http.Error(w, "erreur de creation", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, CreatePaymentOutput{
			UUID:     tx.UUID,
			Provider: "payzen",
			State:    string(tx.Payment.State()),
		})
	default:
		http.Error(w, fmt.Sprintf("provider %q inconnu", provider), http.StatusBadRequest)
	}
}

// isDomainErr identifie les erreurs métier remontées par domain.New à
// travers l'enveloppement fmt.Errorf du provider. Utilisé pour choisir
// entre 400 (input invalide) et 500 (infra).
func isDomainErr(err error) bool {
	return errors.Is(err, domain.ErrInvalidAmount) ||
		errors.Is(err, domain.ErrInvalidCurrency) ||
		errors.Is(err, domain.ErrInvalidPayment)
}

func (h *Handler) listPayments(w http.ResponseWriter, _ *http.Request) {
	txs, err := h.store.AllTransactions()
	if err != nil {
		h.logger.Error("api_store_failure", "op", "AllTransactions", "err", err)
		http.Error(w, "store failure", http.StatusInternalServerError)
		return
	}
	out := make([]PaymentSummary, 0, len(txs))
	for _, tx := range txs {
		out = append(out, toPaymentSummary(tx))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	tx, err := h.store.ByUUID(uuid)
	if err != nil {
		h.logger.Error("api_store_failure", "op", "ByUUID", "err", err)
		http.Error(w, "store failure", http.StatusInternalServerError)
		return
	}
	if tx == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toPaymentDetail(tx))
}

// deletePayment supprime un paiement précis. Idempotent : un UUID
// inconnu retourne 204 (l'état demandé est atteint : ce paiement
// n'existe pas). 500 uniquement sur erreur d'infra.
//
// Utilise en priorité le PaymentRepository cross-provider (permet de
// supprimer un paiement Stripe depuis la même route). Fallback sur
// le store payzen si PaymentRepo n'est pas configuré (mode mémoire).
func (h *Handler) deletePayment(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if uuid == "" {
		http.Error(w, "uuid manquant", http.StatusBadRequest)
		return
	}
	var err error
	if h.paymentRepo != nil {
		err = h.paymentRepo.DeleteByUUID(uuid)
	} else {
		err = h.store.Delete(uuid)
	}
	if err != nil {
		h.logger.Error("api_store_failure", "op", "Delete", "err", err, "uuid", uuid)
		http.Error(w, "store failure", http.StatusInternalServerError)
		return
	}
	h.publisher.Publish(bus.Event{
		Type: "payment_deleted",
		At:   time.Now().UTC(),
		Data: map[string]any{"uuid": uuid},
	})
	w.WriteHeader(http.StatusNoContent)
}

// deletePayments supprime tous les paiements. Filtrable par provider
// via le query param ?provider=payzen — sans filtre, purge complète
// cross-provider.
//
// Retourne 200 avec le compteur du nombre supprimé, pour que l'UI
// puisse afficher un feedback (« 42 paiements supprimés »).
func (h *Handler) deletePayments(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")

	var (
		deleted int
		err     error
	)
	switch {
	case h.paymentRepo != nil && provider != "":
		deleted, err = h.paymentRepo.DeleteByProvider(provider)
	case h.paymentRepo != nil:
		deleted, err = h.paymentRepo.DeleteAll()
	case provider == "payzen" || provider == "":
		// Mode mémoire — seul le store payzen existe, DeleteAll s'y
		// applique. Un provider différent est traité comme un no-op
		// (aucune entrée à supprimer chez nous).
		deleted, err = h.store.DeleteAllTransactions()
	default:
		deleted = 0
	}
	if err != nil {
		h.logger.Error("api_store_failure", "op", "DeleteAll", "err", err, "provider", provider)
		http.Error(w, "store failure", http.StatusInternalServerError)
		return
	}
	h.publisher.Publish(bus.Event{
		Type: "payments_purged",
		At:   time.Now().UTC(),
		Data: map[string]any{
			"provider": provider,
			"deleted":  deleted,
		},
	})
	writeJSON(w, http.StatusOK, map[string]int{"deleted": deleted})
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
	rec, ok := h.queue.WebhookByID(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toWebhookDetail(rec))
}

// replayWebhook re-enqueue un webhook existant. Le body est renvoyé
// tel quel (byte-pour-byte, avec la même signature) — le marchand
// reçoit exactement ce qu'il aurait reçu la première fois. Un nouvel
// ID de livraison est attribué pour distinguer les tentatives dans
// l'historique.
func (h *Handler) replayWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := h.queue.WebhookByID(id)
	if !ok {
		http.Error(w, "webhook not found", http.StatusNotFound)
		return
	}
	wh := rec.Webhook
	wh.ID = "replay-" + id + "-" + time.Now().UTC().Format("150405.000000")
	wh.Attempts = 0
	wh.Delay = 0
	wh.CreatedAt = time.Now().UTC()
	wh.LastTryAt = time.Time{}
	if err := h.queue.Enqueue(wh); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusAccepted, ReplayWebhookResponse{NewDeliveryID: wh.ID})
}

// simulatePayment déclenche une simulation de retour navigateur ou
// d'IPN sur un paiement identifié par son UUID. L'UI n'a pas besoin
// du formToken (privé). Wrapper de payzen.Handler.Simulate.
func (h *Handler) simulatePayment(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		http.Error(w, "payzen handler not configured", http.StatusServiceUnavailable)
		return
	}
	uuid := r.PathValue("uuid")
	tx, err := h.store.ByUUID(uuid)
	if err != nil {
		h.logger.Error("api_store_failure", "op", "ByUUID", "err", err)
		http.Error(w, "store failure", http.StatusInternalServerError)
		return
	}
	if tx == nil {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}

	var req SimulatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Channel == "" {
		req.Channel = "browserReturn"
	}
	if req.Channel != "browserReturn" && req.Channel != "ipn" {
		http.Error(w, "channel doit être browserReturn ou ipn", http.StatusBadRequest)
		return
	}

	opts := payzen.BrowserReturnOpts{
		Outcome:       req.Outcome,
		CardBrand:     req.CardBrand,
		ThreeDSStatus: req.ThreeDSStatus,
		ErrorCode:     req.ErrorCode,
		ErrorMessage:  req.ErrorMessage,
	}

	input := payzen.SimulateInput{
		FormToken:  tx.FormToken,
		AnswerType: "V4/Payment",
		Opts:       opts,
	}
	switch req.Channel {
	case "browserReturn":
		input.URLOverride = req.ReturnURL
		input.FallbackURL = func(tx *payzen.Transaction) string { return tx.ReturnURL }
	case "ipn":
		input.URLOverride = req.NotificationURL
		input.FallbackURL = func(tx *payzen.Transaction) string { return tx.NotificationURL }
	}

	hash, deliveryID, err := h.payzenHandler.Simulate(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, SimulatePaymentResponse{
		DeliveryID: deliveryID, KrHash: hash, Channel: req.Channel,
	})
}

// streamEvents ouvre un flux Server-Sent Events. Chaque événement du
// bus est sérialisé en JSON et envoyé au format standard
//
//	id: <N>\n
//	data: <json>\n\n
//
// L'ID monotone permet au navigateur de renvoyer automatiquement
// l'header Last-Event-ID lors d'une reconnexion — le handler relit
// alors le ring buffer via bus.SnapshotSince pour rattraper ce que
// le client a manqué pendant sa déconnexion.
//
// La séquence Subscribe → SnapshotSince → catchup → live avec filtre
// e.ID > highWater garantit ni doublon ni trou (voir bus.SnapshotSince).
//
// La connexion se ferme quand le client se déconnecte (via
// r.Context()) ou quand le serveur s'arrête.
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

	// Catch-up : replay des events publiés après Last-Event-ID vu par
	// le client. La séquence Subscribe puis SnapshotSince impose
	// l'ordre nécessaire au filtrage anti-doublon plus bas.
	lastID := parseLastEventID(r.Header.Get("Last-Event-ID"))
	catchup, highWater := h.publisher.SnapshotSince(lastID)

	// Envoi initial d'un commentaire SSE pour flusher les headers.
	if _, err := w.Write([]byte(": stream open\n\n")); err != nil {
		return
	}
	flusher.Flush()

	for _, evt := range catchup {
		if !writeEvent(w, flusher, evt, h.logger) {
			return
		}
	}

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
			// Un event capturé par le channel avant/pendant SnapshotSince
			// est déjà dans le catch-up : on le filtre ici.
			if evt.ID <= highWater {
				continue
			}
			if !writeEvent(w, flusher, evt, h.logger) {
				return
			}
		}
	}
}

// parseLastEventID lit le header standard SSE "Last-Event-ID" et
// retourne 0 (= tout envoyer) en cas d'absence ou de valeur invalide.
// Pas d'erreur remontée : un client qui envoie une valeur cassée est
// simplement traité comme un premier connect.
func parseLastEventID(raw string) uint64 {
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// writeEvent sérialise et écrit un event au format SSE avec son ID.
// Retourne false si l'écriture a échoué — le handler doit alors
// abandonner la connexion (client parti).
func writeEvent(w http.ResponseWriter, flusher http.Flusher, evt bus.Event, logger *slog.Logger) bool {
	payload := map[string]any{
		"type": evt.Type,
		"at":   evt.At,
		"data": evt.Data,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("sse_marshal_failed", "err", err)
		return true
	}
	// Format standard SSE : "id: N\ndata: {...}\n\n". Le navigateur
	// mémorise le dernier id vu et le renvoie automatiquement via
	// Last-Event-ID à la prochaine (re)connexion EventSource.
	if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", evt.ID, raw); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// -----------------------------------------------------------------------------
// Convertisseurs internes → DTOs API
// -----------------------------------------------------------------------------

func toPaymentSummary(tx *payzen.Transaction) PaymentSummary {
	return PaymentSummary{
		UUID:      tx.UUID,
		Provider:  "payzen",
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
