// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sprimault/paysim/internal/domain"
)

// Handler regroupe l'etat necessaire pour servir les endpoints REST V4
// de PayZen. Construit dans cmd/paysim/main.go, injecte au serveur HTTP.
type Handler struct {
	store  *Store
	logger *slog.Logger
}

// NewHandler assemble le multiplexeur des endpoints PayZen V4, protege
// par un middleware Basic Auth. Le multiplexeur retourne est branchable
// tel quel sur un http.Server.
//
// Le prefixe /api-payment/V4/ est celui de PayZen reel — les clients
// doivent pouvoir pointer sur Paysim en changeant uniquement l'hote.
func NewHandler(store *Store, logger *slog.Logger) http.Handler {
	h := &Handler{store: store, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api-payment/V4/Charge/CreatePayment", h.createPayment)
	mux.HandleFunc("POST /api-payment/V4/Charge/UpdatePayment", h.updatePayment)
	mux.HandleFunc("POST /api-payment/V4/Charge/CreateSubscription", h.createSubscription)
	mux.HandleFunc("POST /api-payment/V4/Transaction/Get", h.getTransaction)
	mux.HandleFunc("POST /api-payment/V4/Subscription/Get", h.getSubscription)

	return withBasicAuth(mux, logger)
}

// withBasicAuth applique un controle Basic Auth permissif : toute
// paire user:pass non vide est acceptee. C'est coherent avec la
// nature simulateur — on ne veut pas de vrai controle d'acces, juste
// signaler l'absence de header et laisser une trace du user utilise
// pour l'observabilite.
//
// Une validation stricte contre PAYSIM_PAYZEN_USERNAME / _PASSWORD
// configures pourra etre ajoutee plus tard sans breaking change.
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

// createPayment traite POST /api-payment/V4/Charge/CreatePayment.
// Cree un domain.Payment, l'associe a un formToken opaque genere
// cote Paysim, stocke le contexte, retourne le formToken au marchand.
func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var req CreatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, ErrCodeInvalidRequest, "corps JSON invalide")
		return
	}

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
		FormToken:  token,
		UUID:       uuid,
		OrderID:    req.OrderID,
		Amount:     req.Amount,
		Currency:   req.Currency,
		FormAction: req.FormAction,
		Customer:   req.Customer,
		Metadata:   req.Metadata,
		Payment:    payment,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	h.store.Save(tx)

	h.writeSuccess(w, CreatePaymentAnswer{FormToken: token})
}

// updatePayment traite POST /api-payment/V4/Charge/UpdatePayment. Met
// a jour le contexte associe a un formToken existant : coordonnees
// client, metadata. N'affecte pas l'etat du domain.Payment (toujours
// "initiated" tant que le paiement n'a pas ete confirme).
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
	tx := h.store.ByToken(req.FormToken)
	if tx == nil {
		h.writeError(w, ErrCodeTokenUnknown, "formToken inconnu")
		return
	}

	// Mise a jour non destructive : seuls les champs fournis sont
	// remplaces, on ne repasse pas les autres a leur zero-value.
	if req.Customer != (Customer{}) {
		tx.Customer = req.Customer
	}
	if req.Metadata != nil {
		tx.Metadata = req.Metadata
	}
	tx.UpdatedAt = time.Now().UTC()
	h.store.Save(tx)

	h.writeSuccess(w, UpdatePaymentAnswer{FormToken: tx.FormToken})
}

// createSubscription traite POST /api-payment/V4/Charge/CreateSubscription.
// Stub minimaliste en phase 1 : stocke le contexte de l'abonnement,
// retourne un subscriptionId. Aucune mecanique de facturation periodique
// n'est simulee — a etendre si le besoin s'exprime.
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
	if !isCurrencyCode(req.Currency) {
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
	h.store.SaveSubscription(sub)

	h.writeSuccess(w, CreateSubscriptionAnswer{SubscriptionID: subID})
}

// getSubscription traite POST /api-payment/V4/Subscription/Get. Comme
// getTransaction, un ID inconnu produit un HTTP 200 avec status ERROR —
// respect du contrat PayZen (invariant 3).
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
	sub := h.store.SubscriptionByID(req.SubscriptionID)
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

// isCurrencyCode duplique la logique de domain.isCurrencyCode (non
// exportee la-bas). Duplication acceptee car triviale et evite de
// faire remonter cette validation dans le paquet domain hors du
// perimetre de ce vertical.
func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}

// getTransaction traite POST /api-payment/V4/Transaction/Get. Retourne
// le statut d'une transaction indexee par UUID. Un UUID inconnu produit
// une reponse HTTP 200 avec status ERROR — c'est le contrat PayZen,
// pas un 404 (reproduction du protocole tel quel, invariant 3).
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

	tx := h.store.ByUUID(req.UUID)
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

// writeSuccess emet une reponse 200 avec status=SUCCESS et answer serialise.
func (h *Handler) writeSuccess(w http.ResponseWriter, answer any) {
	raw, err := json.Marshal(answer)
	if err != nil {
		// Erreur interne rare — un json.Marshal echoue sur des cycles
		// ou des types non serialisables. On logue et on renvoie
		// status ERROR generique.
		h.logger.Error("payzen_marshal_failed", "err", err)
		h.writeError(w, ErrCodeInvalidRequest, "serialisation reponse impossible")
		return
	}
	resp := APIResponse{Status: "SUCCESS", Answer: raw}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeError emet une reponse 200 avec status=ERROR — c'est le format
// PayZen (pas de 4xx sur erreur metier, reserves aux vrais defauts de
// requete gerents par le serveur HTTP lui-meme).
func (h *Handler) writeError(w http.ResponseWriter, code, message string) {
	raw, _ := json.Marshal(APIError{ErrorCode: code, ErrorMessage: message})
	resp := APIResponse{Status: "ERROR", Answer: raw}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeDomainError traduit une erreur sentinelle du domaine en code
// Paysim. Concentre la traduction domain → protocol en un seul point,
// evite que chaque handler ait a la refaire.
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

// newFormToken genere un formToken opaque cote marchand : 32 caracteres
// hexadecimaux issus de 16 octets aleatoires. Format arbitraire —
// PayZen utilise du base64 URL-safe, mais le marchand traite ce token
// comme une chaine opaque, il ne fait aucun controle de format.
func newFormToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// newUUID genere un identifiant UUID v4 conformement a la RFC 4122 :
// 128 bits aleatoires avec bits de version et variant fixes. Format
// canonique 8-4-4-4-12 en hexadecimal minuscule. Aucune dependance
// externe — cohérent avec la preference stdlib du projet.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
