// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/chaos"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
)

// HandlerConfig regroupe les parametres injectes au Handler. Une seule
// struct plutot que 3 parametres positionnels dans NewHandler — plus
// lisible et extensible sans breaking change.
type HandlerConfig struct {
	// HMACKey est la cle HMAC-SHA-256 utilisee pour signer kr-hash sur
	// les retours navigateur et webhooks IPN. Vide = les endpoints de
	// simulation retournent une erreur claire au premier appel.
	HMACKey string

	// APIToken protege les endpoints de simulation via Bearer. Vide =
	// API de controle ouverte (mode local explicite, cf. CLAUDE.md).
	APIToken string

	// Chaos porte l'injection de pannes appliquee sur les endpoints
	// REST V4 uniquement (pas sur l'API de controle /paysim/simulate/*).
	// Nil = pas de chaos (invariant 5 par defaut).
	Chaos *chaos.Chaos

	// Publisher est le bus d'evenements pour alimenter l'UI et les
	// abonnes SSE. Nil = pas de publication, comportement inchange.
	Publisher *bus.Bus

	// DefaultCallbackURL est l'URL utilisee par simulate() quand ni la
	// requete ni la transaction ne fournissent d'URL cible. Alimente
	// depuis PAYSIM_CALLBACK_URL par cmd/paysim/main.go — c'est le sens
	// meme de cette variable, un endroit ou envoyer les webhooks par
	// defaut quand un marchand de test ne configure rien. Un warn est
	// logue chaque fois que ce fallback declenche pour que le dev sache
	// ou part le webhook.
	DefaultCallbackURL string
}

// Handler regroupe l'etat necessaire pour servir les endpoints REST V4
// de PayZen et les endpoints de controle Paysim. Construit dans
// cmd/paysim/main.go, injecte au serveur HTTP.
type Handler struct {
	store  Store
	queue  *delivery.Queue
	logger *slog.Logger
	cfg    HandlerConfig
}

// NewHandler instancie un Handler PayZen. Le multiplexeur HTTP est
// obtenu ensuite via Routes() — deux étapes pour que les consommateurs
// qui ont besoin d'un *Handler concret (ex. api.Handler pour Simulate)
// puissent le récupérer sans caster un http.Handler.
func NewHandler(store Store, queue *delivery.Queue, logger *slog.Logger, cfg HandlerConfig) *Handler {
	return &Handler{store: store, queue: queue, logger: logger, cfg: cfg}
}

// storeErr traduit une erreur de persistance en réponse PayZen —
// status=ERROR + PAYSIM_STORE_FAILURE. Logue au niveau Error côté
// serveur pour tracer la vraie cause.
func (h *Handler) storeErr(w http.ResponseWriter, op string, err error) {
	h.logger.Error("payzen_store_failure", "op", op, "err", err)
	h.writeError(w, ErrCodeStoreFailure, "erreur de persistance")
}

// Routes assemble le multiplexeur complet : endpoints REST V4 PayZen
// (proteges par Basic Auth permissive) sous /api-payment/V4/*, et
// endpoints de controle Paysim (Bearer conditionnel) sous
// /paysim/simulate/*.
//
// Le prefixe /api-payment/V4/ est celui de PayZen reel — les clients
// doivent pouvoir pointer sur Paysim en changeant uniquement l'hote.
// Le prefixe /paysim/simulate/ est propre a Paysim.
func (h *Handler) Routes() http.Handler {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api-payment/V4/Charge/CreatePayment", h.createPayment)
	apiMux.HandleFunc("POST /api-payment/V4/Charge/UpdatePayment", h.updatePayment)
	apiMux.HandleFunc("POST /api-payment/V4/Charge/CreateSubscription", h.createSubscription)
	apiMux.HandleFunc("POST /api-payment/V4/Transaction/Get", h.getTransaction)
	apiMux.HandleFunc("POST /api-payment/V4/Subscription/Get", h.getSubscription)

	simMux := http.NewServeMux()
	simMux.HandleFunc("POST /paysim/simulate/browserReturn", h.browserReturn)
	simMux.HandleFunc("POST /paysim/simulate/ipn", h.ipn)

	// Chaos s'applique uniquement sur les endpoints REST V4 (ce que
	// PayZen exposerait vraiment) — pas sur l'API de contrôle Paysim,
	// dont les défauts ne sont pas ce qu'on veut simuler.
	apiWithChaos := h.cfg.Chaos.Middleware(withBasicAuth(apiMux, h.logger))

	mainMux := http.NewServeMux()
	mainMux.Handle("/api-payment/V4/", apiWithChaos)
	mainMux.Handle("/paysim/simulate/", withBearerToken(simMux, h.cfg.APIToken, h.logger))

	return mainMux
}

// withBasicAuth applique un controle Basic Auth permissif : toute
// paire user:pass non vide est acceptee. Coherent avec la nature
// simulateur — on ne veut pas de vrai controle d'acces, juste signaler
// l'absence de header et laisser une trace du user pour l'observabilite.
func withBasicAuth(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user == "" || pass == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Paysim"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		logger.Debug("payzen_basic_auth", "user", user, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// withBearerToken protege un mux via un token Bearer configure. Si
// expected est vide, laisse passer sans controle (mode local explicite).
// Sinon, exige Authorization: Bearer <expected> avec comparaison en
// temps constant pour eviter les timing attacks — la meme famille
// d'attaque que la verification d'un kr-hash.
func withBearerToken(next http.Handler, expected string, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expected == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		want := "Bearer " + expected
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Paysim"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			logger.Debug("paysim_bearer_denied", "path", r.URL.Path)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// createPayment traite POST /api-payment/V4/Charge/CreatePayment.
// Cree un domain.Payment, l'associe a un formToken opaque genere
// cote Paysim, stocke le contexte (dont ReturnURL/NotificationURL
// si fournies), retourne le formToken au marchand.
func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var req CreatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "corps JSON invalide")
		return
	}

	// Magic amount : si le montant se termine par 03, on applique une
	// latence extrême avant même d'aller plus loin — provoque un
	// timeout côté client marchand sans changer le résultat de
	// l'appel. Voir chaos.MagicLatencyMs.
	h.cfg.Chaos.Sleep(r.Context(), chaos.MagicLatencyMs(req.Amount))

	uuid, err := newUUID()
	if err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "generation uuid impossible")
		return
	}
	payment, err := domain.New(uuid, req.Amount, req.Currency)
	if err != nil {
		h.writeDomainError(w, err)
		return
	}
	token, err := newFormToken()
	if err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "generation formToken impossible")
		return
	}

	now := time.Now().UTC()
	tx := &Transaction{
		FormToken:       token,
		UUID:            uuid,
		OrderID:         req.OrderID,
		Amount:          req.Amount,
		Currency:        req.Currency,
		FormAction:      req.FormAction,
		Customer:        req.Customer,
		Metadata:        req.Metadata,
		Payment:         payment,
		ReturnURL:       req.ReturnURL,
		NotificationURL: req.NotificationURL,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := h.store.Save(tx); err != nil {
		h.storeErr(w, "createPayment.Save", err)
		return
	}

	h.cfg.Publisher.Publish(bus.Event{
		Type: "payment_created",
		At:   now,
		Data: map[string]any{
			"uuid":    uuid,
			"orderId": req.OrderID,
			"amount":  req.Amount,
			"currency": req.Currency,
		},
	})

	h.writeSuccess(w, CreatePaymentAnswer{FormToken: token})
}

// updatePayment traite POST /api-payment/V4/Charge/UpdatePayment. Met
// a jour le contexte associe a un formToken existant : coordonnees
// client, metadata. N'affecte pas l'etat du domain.Payment.
func (h *Handler) updatePayment(w http.ResponseWriter, r *http.Request) {
	var req UpdatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "corps JSON invalide")
		return
	}
	if req.FormToken == "" {
		h.writeError(w, ErrCodeInvalidRequest, "formToken manquant")
		return
	}
	tx, err := h.store.ByToken(req.FormToken)
	if err != nil {
		h.storeErr(w, "updatePayment.ByToken", err)
		return
	}
	if tx == nil {
		h.writeError(w, ErrCodeTokenUnknown, "formToken inconnu")
		return
	}

	if req.Customer != (Customer{}) {
		tx.Customer = req.Customer
	}
	if req.Metadata != nil {
		tx.Metadata = req.Metadata
	}
	tx.UpdatedAt = time.Now().UTC()
	if err := h.store.Save(tx); err != nil {
		h.storeErr(w, "updatePayment.Save", err)
		return
	}

	h.writeSuccess(w, UpdatePaymentAnswer{FormToken: tx.FormToken})
}

// createSubscription traite POST /api-payment/V4/Charge/CreateSubscription.
// Stub minimaliste en phase 1 : stocke le contexte de l'abonnement,
// retourne un subscriptionId. Aucune mecanique de facturation periodique
// n'est simulee.
func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) {
	var req CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "corps JSON invalide")
		return
	}
	if req.Amount <= 0 {
		h.writeError(w, ErrCodeInvalidAmount, "montant invalide")
		return
	}
	if !domain.IsCurrencyCode(req.Currency) {
		h.writeError(w, ErrCodeInvalidCurrency, "devise invalide")
		return
	}
	if req.PaymentMethodToken == "" {
		h.writeError(w, ErrCodeInvalidRequest, "paymentMethodToken manquant")
		return
	}
	subID, err := newUUID()
	if err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "generation subscriptionId impossible")
		return
	}

	sub := &Subscription{
		ID:                 subID,
		OrderID:            req.OrderID,
		Amount:             req.Amount,
		Currency:           req.Currency,
		PaymentMethodToken: req.PaymentMethodToken,
		EffectDate:         req.EffectDate,
		Rrule:              req.Rrule,
		Metadata:           req.Metadata,
		CreatedAt:          time.Now().UTC(),
	}
	if err := h.store.SaveSubscription(sub); err != nil {
		h.storeErr(w, "createSubscription.SaveSubscription", err)
		return
	}

	h.writeSuccess(w, CreateSubscriptionAnswer{SubscriptionID: subID})
}

// getSubscription traite POST /api-payment/V4/Subscription/Get.
func (h *Handler) getSubscription(w http.ResponseWriter, r *http.Request) {
	var req SubscriptionGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "corps JSON invalide")
		return
	}
	if req.SubscriptionID == "" {
		h.writeError(w, ErrCodeInvalidRequest, "subscriptionId manquant")
		return
	}
	sub, err := h.store.SubscriptionByID(req.SubscriptionID)
	if err != nil {
		h.storeErr(w, "getSubscription.SubscriptionByID", err)
		return
	}
	if sub == nil {
		h.writeError(w, ErrCodeSubscriptionUnknown, "abonnement introuvable")
		return
	}

	answer := SubscriptionGetAnswer{
		SubscriptionID:     sub.ID,
		OrderID:            sub.OrderID,
		Amount:             sub.Amount,
		Currency:           sub.Currency,
		EffectDate:         sub.EffectDate,
		Rrule:              sub.Rrule,
		PaymentMethodToken: sub.PaymentMethodToken,
		CreationDate:       sub.CreatedAt.Format(time.RFC3339),
		Metadata:           sub.Metadata,
	}
	h.writeSuccess(w, answer)
}

// getTransaction traite POST /api-payment/V4/Transaction/Get. Retourne
// le statut d'une transaction indexee par UUID. Un UUID inconnu produit
// une reponse HTTP 200 avec status ERROR — respect du contrat PayZen
// (invariant 3).
func (h *Handler) getTransaction(w http.ResponseWriter, r *http.Request) {
	var req TransactionGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "corps JSON invalide")
		return
	}
	if req.UUID == "" {
		h.writeError(w, ErrCodeInvalidRequest, "uuid manquant")
		return
	}

	tx, err := h.store.ByUUID(req.UUID)
	if err != nil {
		h.storeErr(w, "getTransaction.ByUUID", err)
		return
	}
	if tx == nil {
		h.writeError(w, ErrCodeUUIDUnknown, fmt.Sprintf("transaction %q introuvable", req.UUID))
		return
	}

	answer := TransactionGetAnswer{
		UUID:           tx.UUID,
		OrderID:        tx.OrderID,
		Amount:         tx.Amount,
		Currency:       tx.Currency,
		OrderStatus:    tx.Payment.State(),
		CreationDate:   tx.CreatedAt.Format(time.RFC3339),
		LastUpdateDate: tx.UpdatedAt.Format(time.RFC3339),
		Customer:       tx.Customer,
		Metadata:       tx.Metadata,
	}
	h.writeSuccess(w, answer)
}

// browserReturn simule le POST du retour navigateur : PayZen fait
// theoriquement un POST vers l'URL de retour marchand avec kr-answer
// et kr-hash. Le marchand appelle cet endpoint pour declencher la
// simulation d'un tel retour.
func (h *Handler) browserReturn(w http.ResponseWriter, r *http.Request) {
	var req BrowserReturnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeSimulateError(w, err)
		return
	}
	opts := BrowserReturnOpts{
		Outcome:           req.Outcome,
		PaymentMethodType: req.PaymentMethodType,
		CardBrand:         req.CardBrand,
		Wallet:            req.Wallet,
		ThreeDSStatus:     req.ThreeDSStatus,
		ErrorCode:         req.ErrorCode,
		ErrorMessage:      req.ErrorMessage,
		Chaos:             req.Chaos,
		DeliveryDelayMs:   req.DeliveryDelayMs,
	}
	hash, deliveryID, err := h.simulate(req.FormToken, req.ReturnURL, opts, "V4/Payment",
		func(tx *Transaction) string { return tx.ReturnURL })
	if err != nil {
		h.writeSimulateError(w, err)
		return
	}
	h.writeSimulateSuccess(w, BrowserReturnResponse{
		Status: "SUCCESS", DeliveryID: deliveryID, KrHash: hash,
	})
}

// ipn simule le POST du webhook IPN serveur-a-serveur. Meme mecanique
// que browserReturn, URL cible differente : NotificationURL au lieu
// de ReturnURL.
func (h *Handler) ipn(w http.ResponseWriter, r *http.Request) {
	var req IPNRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeSimulateError(w, err)
		return
	}
	opts := BrowserReturnOpts{
		Outcome:           req.Outcome,
		PaymentMethodType: req.PaymentMethodType,
		CardBrand:         req.CardBrand,
		Wallet:            req.Wallet,
		ThreeDSStatus:     req.ThreeDSStatus,
		ErrorCode:         req.ErrorCode,
		ErrorMessage:      req.ErrorMessage,
		Chaos:             req.Chaos,
		DeliveryDelayMs:   req.DeliveryDelayMs,
	}
	hash, deliveryID, err := h.simulate(req.FormToken, req.NotificationURL, opts, "V4/Payment",
		func(tx *Transaction) string { return tx.NotificationURL })
	if err != nil {
		h.writeSimulateError(w, err)
		return
	}
	h.writeSimulateSuccess(w, IPNResponse{
		Status: "SUCCESS", DeliveryID: deliveryID, KrHash: hash,
	})
}

// SimulateInput regroupe les paramètres de Simulate. Passé en struct
// (au lieu de 5 paramètres positionnels) pour rester lisible côté
// appelants — les handlers HTTP internes et l'API UI.
type SimulateInput struct {
	FormToken   string
	URLOverride string                    // vide = fallback sur la Transaction
	AnswerType  string                    // "V4/Payment"
	Opts        BrowserReturnOpts
	FallbackURL func(*Transaction) string // ex : func(tx){return tx.ReturnURL}
}

// Simulate est la logique de simulation d'un retour signé. Exportée
// pour que le paquet api puisse la consommer sans passer par un
// self-loopback HTTP. Valide, fait transiter le domain.Payment,
// construit le kr-answer, signe, enqueue le webhook via
// internal/delivery. Retourne le hash calculé et l'id de livraison,
// ou une erreur (convertie en 400 par les handlers HTTP).
func (h *Handler) Simulate(in SimulateInput) (hash, deliveryID string, err error) {
	return h.simulate(in.FormToken, in.URLOverride, in.Opts, in.AnswerType, in.FallbackURL)
}

// simulate est la logique commune aux deux endpoints de simulation :
// valide la requete, fait transiter le domain.Payment, construit le
// kr-answer, signe, enqueue le webhook via internal/delivery.
// Retourne le hash calcule et l'id de livraison, ou une erreur qui
// sera convertie en 400 par le handler.
func (h *Handler) simulate(
	formToken, urlOverride string,
	opts BrowserReturnOpts,
	answerType string,
	fallbackURL func(*Transaction) string,
) (hash, deliveryID string, err error) {
	if h.cfg.HMACKey == "" {
		return "", "", errors.New("simulation impossible : PAYSIM_PAYZEN_HMAC_KEY non configuree")
	}
	if formToken == "" {
		return "", "", errors.New("formToken manquant")
	}
	if _, ok := outcomeSpecs[opts.Outcome]; !ok {
		return "", "", fmt.Errorf("outcome %q inconnu", opts.Outcome)
	}
	tx, err := h.store.ByToken(formToken)
	if err != nil {
		return "", "", fmt.Errorf("store ByToken: %w", err)
	}
	if tx == nil {
		return "", "", errors.New("formToken inconnu")
	}
	// Magic amount : si le montant se termine par 01, l'outcome
	// demandé est forcé à UNPAID quel que soit le paramètre client.
	// Cohérent avec l'invariant 5 : le chaos par valeur magique est
	// un mode d'activation légitime, sans besoin de config globale.
	if magic := chaos.MagicOutcome(tx.Amount); magic != "" {
		opts.Outcome = magic
	}
	targetURL := urlOverride
	if targetURL == "" {
		targetURL = fallbackURL(tx)
	}
	if targetURL == "" && h.cfg.DefaultCallbackURL != "" {
		targetURL = h.cfg.DefaultCallbackURL
		h.logger.Warn("simulate_fallback_callback_url",
			"uuid", tx.UUID,
			"url", targetURL,
		)
	}
	if targetURL == "" {
		return "", "", errors.New("URL cible manquante : ni fournie dans la requete, ni stockee dans la transaction, ni PAYSIM_CALLBACK_URL configuree")
	}
	if err := applyOutcome(tx, opts.Outcome, opts.ErrorMessage); err != nil {
		return "", "", fmt.Errorf("transition domain: %w", err)
	}
	tx.UpdatedAt = time.Now().UTC()
	if err := h.store.Save(tx); err != nil {
		return "", "", fmt.Errorf("store Save: %w", err)
	}

	h.cfg.Publisher.Publish(bus.Event{
		Type: "payment_state_changed",
		At:   tx.UpdatedAt,
		Data: map[string]any{
			"uuid":    tx.UUID,
			"orderId": tx.OrderID,
			"state":   string(tx.Payment.State()),
			"outcome": opts.Outcome,
		},
	})

	// serverURL vide en phase 1 (arrivera avec cmd/paysim qui saura
	// son propre PublicURL). Mode "TEST" en dur — un simulateur n'a
	// pas de "PRODUCTION".
	answer := buildKrAnswer(tx, opts, "", "TEST")

	deliveryID, err = newUUID()
	if err != nil {
		return "", "", fmt.Errorf("generation deliveryId: %w", err)
	}
	delay := time.Duration(opts.DeliveryDelayMs) * time.Millisecond
	wh, hash, err := buildDeliveryWebhook(deliveryID, targetURL, answer, h.cfg.HMACKey, answerType,
		opts.Chaos.BadSignature, delay)
	if err != nil {
		return "", "", err
	}
	if err := h.queue.Enqueue(wh); err != nil {
		return "", "", fmt.Errorf("enqueue: %w", err)
	}
	// Duplicate : deuxième enqueue du meme webhook — le marchand doit
	// gerer l'idempotence via l'UUID de transaction.
	if opts.Chaos.Duplicate {
		if err := h.queue.Enqueue(wh); err != nil {
			h.logger.Warn("chaos_duplicate_enqueue_failed", "err", err)
		}
	}
	// Race before response : on retarde la reponse HTTP pour laisser
	// le webhook partir en premier. Le client marchand recoit ainsi
	// la notification avant la reponse a son appel de simulation.
	if opts.Chaos.RaceBeforeResponse {
		time.Sleep(500 * time.Millisecond)
	}
	return hash, deliveryID, nil
}

// writeSuccess emet une reponse 200 avec status=SUCCESS et answer serialise.
func (h *Handler) writeSuccess(w http.ResponseWriter, answer any) {
	raw, err := json.Marshal(answer)
	if err != nil {
		h.logger.Error("payzen_marshal_failed", "err", err)
		h.writeError(w, ErrCodeInvalidRequest, "serialisation reponse impossible")
		return
	}
	resp := APIResponse{Status: "SUCCESS", Answer: raw}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeError emet une reponse 200 avec status=ERROR — format PayZen.
func (h *Handler) writeError(w http.ResponseWriter, code, message string) {
	raw, _ := json.Marshal(APIError{ErrorCode: code, ErrorMessage: message})
	resp := APIResponse{Status: "ERROR", Answer: raw}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeDomainError traduit une erreur sentinelle du domaine en code Paysim.
func (h *Handler) writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount):
		h.writeError(w, ErrCodeInvalidAmount, err.Error())
	case errors.Is(err, domain.ErrInvalidCurrency):
		h.writeError(w, ErrCodeInvalidCurrency, err.Error())
	case errors.Is(err, domain.ErrInvalidPayment):
		h.writeError(w, ErrCodeInvalidPayment, err.Error())
	default:
		h.writeError(w, ErrCodeInvalidRequest, err.Error())
	}
}

// writeSimulateSuccess emet une reponse JSON de succes pour les API
// de controle Paysim (format plat, pas le wrapper PayZen).
func (h *Handler) writeSimulateSuccess(w http.ResponseWriter, resp any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeSimulateError emet une reponse 400 pour les API de controle.
// Les erreurs ici sont fonctionnelles (mauvaise requete, formToken
// inconnu, HMAC manquant), pas des erreurs metier PayZen — un 4xx
// HTTP est donc approprie, contrairement aux endpoints REST V4.
func (h *Handler) writeSimulateError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// newFormToken genere un formToken opaque : 32 caracteres hexadecimaux
// issus de 16 octets aleatoires. Format arbitraire — PayZen utilise du
// base64 URL-safe, mais le marchand traite ce token comme une chaine
// opaque, il ne fait aucun controle de format.
func newFormToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// newUUID genere un identifiant UUID v4 conformement a la RFC 4122 :
// 128 bits aleatoires avec bits de version et variant fixes. Format
// canonique 8-4-4-4-12 en hexadecimal minuscule.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
