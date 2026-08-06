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
	"github.com/sprimault/paysim/internal/webui"
)

// Deps regroupe les dépendances de l'API UI. Struct plutôt que
// paramètres positionnels — plus lisible et extensible sans
// breaking change côté cmd/paysim.
//
// SubscriptionRepo et PaymentMethodRepo sont optionnels — nil en
// mode mémoire, les endpoints listing correspondants retournent alors
// une liste vide (comportement cohérent avec le mode dégradé mémoire :
// les entités survivent au run mais ne sont pas listées globalement).
type Deps struct {
	// Store est le magasin des transactions PayZen, seul disponible en
	// mode mémoire.
	Store payzen.Store

	// Les trois dépôts cross-provider sont nil en mode mémoire. Les
	// endpoints qui en dépendent retournent alors une liste vide ou
	// retombent sur Store — un mode dégradé assumé, pas une panne :
	// les entités survivent au run, elles ne sont simplement pas
	// listables globalement.
	PaymentRepo       store.PaymentRepository
	SubscriptionRepo  store.SubscriptionRepository
	PaymentMethodRepo store.PaymentMethodRepository

	// Queue porte la file de livraison et l'historique des webhooks.
	Queue *delivery.Queue

	// Publisher diffuse les événements vers les abonnés SSE, ce qui
	// tient l'interface à jour sans qu'elle interroge en boucle.
	Publisher *bus.Bus

	// Logger reçoit les journaux structurés de l'API.
	Logger *slog.Logger

	// Token protège l'API par Bearer. Vide, l'API est ouverte — le
	// comportement voulu en local, et la raison pour laquelle activer
	// ce jeton désactive l'interface web.
	Token string

	// PayzenHandler permet de créer un paiement sans repasser par HTTP.
	// L'API de contrôle appelle directement l'adaptateur plutôt que de
	// se requêter elle-même.
	PayzenHandler *payzen.Handler
}

// Handler regroupe les dépendances nécessaires pour servir les
// endpoints API et SSE. Instancié dans cmd/paysim/main.go.
type Handler struct {
	store             payzen.Store
	paymentRepo       store.PaymentRepository
	subscriptionRepo  store.SubscriptionRepository
	paymentMethodRepo store.PaymentMethodRepository
	queue             *delivery.Queue
	publisher         *bus.Bus
	logger            *slog.Logger
	token             string
	payzenHandler     *payzen.Handler
}

// NewHandler retourne un http.Handler qui multiplexe les endpoints
// UI sous /paysim/api/v1/*, protégé par Bearer si Token non vide.
func NewHandler(deps Deps) http.Handler {
	h := &Handler{
		store:             deps.Store,
		paymentRepo:       deps.PaymentRepo,
		subscriptionRepo:  deps.SubscriptionRepo,
		paymentMethodRepo: deps.PaymentMethodRepo,
		queue:             deps.Queue,
		publisher:         deps.Publisher,
		logger:            deps.Logger,
		token:             deps.Token,
		payzenHandler:     deps.PayzenHandler,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /paysim/api/v1/payments", h.listPayments)
	mux.HandleFunc("POST /paysim/api/v1/payments", h.createPayment)
	mux.HandleFunc("DELETE /paysim/api/v1/payments", h.deletePayments)
	mux.HandleFunc("GET /paysim/api/v1/payments/{uuid}", h.getPayment)
	mux.HandleFunc("DELETE /paysim/api/v1/payments/{uuid}", h.deletePayment)
	mux.HandleFunc("POST /paysim/api/v1/payments/{uuid}/simulate", h.simulatePayment)
	mux.HandleFunc("POST /paysim/api/v1/reset", h.reset)
	mux.HandleFunc("GET /paysim/api/v1/webhooks", h.listWebhooks)
	mux.HandleFunc("GET /paysim/api/v1/webhooks/{id}", h.getWebhook)
	mux.HandleFunc("POST /paysim/api/v1/webhooks/{id}/replay", h.replayWebhook)
	mux.HandleFunc("GET /paysim/api/v1/payment-methods", h.listPaymentMethods)
	mux.HandleFunc("GET /paysim/api/v1/payment-methods/{token}", h.getPaymentMethod)
	mux.HandleFunc("POST /paysim/api/v1/payment-methods/{token}/revoke", h.revokePaymentMethod)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions", h.createSubscription)
	mux.HandleFunc("GET /paysim/api/v1/subscriptions", h.listSubscriptions)
	mux.HandleFunc("GET /paysim/api/v1/subscriptions/{id}", h.getSubscription)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions/{id}/trigger-billing", h.triggerBilling)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions/{id}/cancel", h.cancelSubscription)
	mux.HandleFunc("GET /paysim/api/v1/events/stream", h.streamEvents)
	mux.HandleFunc("GET /paysim/api/v1/version", h.getVersion)

	return withBearer(mux, deps.Token, deps.Logger)
}

// getVersion retourne le hash du bundle Vite embarqué. Le front
// interroge cet endpoint pour proposer un rechargement quand une
// nouvelle version est déployée. Public (avant le Bearer serait plus
// propre à terme, mais l'API entière l'est déjà quand PAYSIM_API_TOKEN
// est vide — le comportement attendu en dev).
func (h *Handler) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"hash": webui.Version()})
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
	// UUID identifie le paiement côté Paysim. C'est lui qu'on passe à
	// /payments/{uuid}/simulate, et il se retrouve dans le webhook sous
	// transactions[0].uuid.
	UUID string `json:"uuid"`

	// Provider nomme l'adaptateur qui a matérialisé le paiement
	// ("payzen" aujourd'hui). Sert à filtrer les listes.
	Provider string `json:"provider"`

	// OrderID est la référence de commande choisie par le marchand.
	// Libre et non unique côté Paysim.
	OrderID string `json:"orderId"`

	// Amount est en centimes entiers, jamais en unité monétaire — un
	// paiement de 49,90 € vaut 4990. Zéro est légitime : c'est
	// l'enrôlement pur, qui crée un moyen de paiement sans débiter.
	Amount int64 `json:"amount"`

	// Currency au format ISO 4217 ("EUR").
	Currency string `json:"currency"`

	// State est l'état du domaine, pas le vocabulaire du provider :
	// initiated, authorized, captured, partially_refunded, refunded,
	// declined, expired, chargeback. Voir docs/states.md.
	State string `json:"state"`

	// CreatedAt et UpdatedAt sont en UTC. UpdatedAt bouge à chaque
	// transition ; sur un paiement jamais joué, les deux sont égales.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PaymentDetail ajoute le journal d'événements.
type PaymentDetail struct {
	PaymentSummary

	// Events est le journal complet, dans l'ordre chronologique. Il
	// raconte l'histoire du paiement là où State n'en donne que le
	// dernier mot — un remboursement partiel y laisse une trace même
	// quand l'état ne change pas.
	Events []EventEntry `json:"events"`
}

// EventEntry est une entrée du journal d'événements du domaine.
type EventEntry struct {
	// At est l'instant de l'événement, en UTC.
	At time.Time `json:"at"`

	// Kind est la nature de l'événement — created, authorized,
	// captured, refunded… Le journal est immuable : un remboursement
	// partiel produit un événement même quand l'état ne bouge pas.
	Kind string `json:"kind"`

	// Amount en centimes, renseigné sur les événements qui portent un
	// montant comme un remboursement.
	Amount int64 `json:"amount,omitempty"`

	// Note porte le motif quand il y en a un, par exemple la raison
	// d'un refus.
	Note string `json:"note,omitempty"`
}

// WebhookEntry résume une tentative de livraison — pour la liste UI.
type WebhookEntry struct {
	// ID identifie la tentative de livraison. Un rejeu en produit une
	// nouvelle, avec son propre ID — c'est ce qui permet de suivre
	// chaque essai séparément.
	ID string `json:"id"`

	// URL est la cible effectivement appelée.
	URL string `json:"url"`

	// Status décrit l'acheminement HTTP ("delivered", "failed",
	// "pending"), Outcome le résultat métier annoncé dans le corps
	// ("PAID", "UNPAID"… en vocabulaire provider). Deux questions
	// distinctes : un webhook peut être remis avec succès tout en
	// annonçant un refus.
	Status  string `json:"status"`
	Outcome string `json:"outcome,omitempty"`

	// StatusCode est le code HTTP reçu, zéro quand l'erreur est
	// survenue avant toute réponse — DNS, timeout, connexion refusée.
	// ErrorMsg porte alors le détail.
	StatusCode int    `json:"statusCode,omitempty"`
	ErrorMsg   string `json:"errorMsg,omitempty"`

	// Attempts compte les tentatives sur cette livraison.
	Attempts int `json:"attempts"`

	// CreatedAt est l'entrée en file, CompletedAt la fin de tentative.
	// Leur écart mesure ce qu'a coûté la livraison, délai de chaos
	// compris.
	CreatedAt   time.Time `json:"createdAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// WebhookDetail ajoute les headers et le body pour la vue
// requête/réponse côte à côte.
type WebhookDetail struct {
	WebhookEntry

	// Headers et Body sont ce qui a réellement été envoyé. C'est là que
	// le marchand va vérifier sa signature ou relire le kr-answer —
	// d'où leur absence de la vue liste, qui n'a pas à transporter des
	// corps entiers.
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// CreatePaymentInput est le corps de POST /paysim/api/v1/payments,
// endpoint générique de création cross-provider.
//
// Trois usages selon ce qu'on fournit : ni Card ni token pour un
// paiement classique qui attend d'être joué, Card pour enrôler un
// moyen au passage, ou un token seul pour rejouer sans formulaire.
type CreatePaymentInput struct {
	// Provider choisit l'adaptateur. Vide vaut "payzen", avec un log
	// Debug pour tracer les choix implicites dans un journal chargé.
	Provider string `json:"provider,omitempty"`

	// Amount en centimes entiers — 49,90 € vaut 4990. Zéro est valide
	// et désigne l'enrôlement pur : on enregistre une carte sans rien
	// débiter.
	Amount format.Amount `json:"amount"`

	// Currency en ISO 4217, OrderID libre côté marchand.
	Currency string `json:"currency"`
	OrderID  string `json:"orderId"`

	// FormAction déclare l'intention (PAYMENT, REGISTER,
	// REGISTER_PAY…). Conservée et restituée, mais sans effet sur
	// l'enrôlement : une carte fournie est toujours enregistrée.
	FormAction string `json:"formAction,omitempty"`

	// Customer et Metadata sont restitués tels quels dans le webhook.
	// Metadata est le canal prévu pour rattacher un paiement à un objet
	// métier sans dépendre de l'orderId.
	Customer payzen.Customer   `json:"customer,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`

	// NotificationURL est la cible de l'IPN. Absente, le serveur
	// retombe sur PAYSIM_CALLBACK_URL — ce qui rend les rejeux
	// notifiables sans URL au coup par coup.
	NotificationURL string `json:"notificationUrl,omitempty"`

	// Card enrôle un moyen de paiement. Extension Paysim : le vrai
	// PayZen collecte la carte par le SmartForm client, jamais par
	// l'API marchand.
	Card *payzen.Card `json:"card,omitempty"`

	// PaymentMethodToken déclenche un rejeu one-click sur un moyen déjà
	// enregistré : pas de formulaire, issue immédiate, webhook émis
	// dans la foulée. Prend le pas sur Card si les deux sont fournis.
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`
}

// CreatePaymentOutput est la réponse de création. Elle porte déjà
// l'état, ce qui évite un GET juste après — utile sur un rejeu, où
// l'issue est connue dès le retour HTTP.
type CreatePaymentOutput struct {
	// UUID identifie le paiement créé.
	UUID string `json:"uuid"`

	// Provider nomme l'adaptateur qui l'a matérialisé.
	Provider string `json:"provider"`

	// State est l'état à l'issue de l'appel. initiated sur un paiement
	// qui attend d'être joué ; captured ou declined quand l'issue est
	// immédiate — rejeu one-click, ou autoplay actif.
	State string `json:"state"`

	// PaymentMethodToken est l'alias créé par un enrôlement. Absent sur
	// un paiement refusé : l'annoncer à côté d'un refus laisserait
	// croire qu'il est débitable.
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`
}

// SimulatePaymentRequest est le corps de POST
// /paysim/api/v1/payments/{uuid}/simulate. L'UI n'a pas à connaître
// le formToken interne — Paysim le retrouve depuis l'uuid. Le champ
// channel choisit entre retour navigateur (défaut) et IPN pur.
type SimulatePaymentRequest struct {
	// Outcome est l'issue à jouer : PAID, AUTHORISED, UNPAID, EXPIRED
	// ou ABANDONED. Toute autre valeur est refusée avec la liste des
	// valeurs acceptées.
	Outcome string `json:"outcome"`

	// Channel choisit le canal d'émission : browserReturn (défaut)
	// suit le navigateur du porteur, ipn part serveur à serveur. Deux
	// chemins distincts pour un même kr-answer — c'est ce qui permet
	// de provoquer leur inversion.
	Channel string `json:"channel,omitempty"`

	// ReturnURL et NotificationURL surchargent les cibles de la
	// transaction, selon le canal retenu. Absentes des deux, le
	// serveur retombe sur PAYSIM_CALLBACK_URL.
	ReturnURL       string `json:"returnUrl,omitempty"`
	NotificationURL string `json:"notificationUrl,omitempty"`

	// CardBrand et ThreeDSStatus habillent le webhook : marque
	// annoncée, verdict d'authentification. CardBrand est ignoré dès
	// qu'un moyen enrôlé existe.
	CardBrand     string `json:"cardBrand,omitempty"`
	ThreeDSStatus string `json:"threeDSStatus,omitempty"`

	// ErrorCode et ErrorMessage détaillent un refus.
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Chaos active des modes de panne sur le webhook émis par cet
	// appel — chaque flag indépendant, tous inertes par défaut. Ces
	// modes sont ceux qui n'ont pas de sens côté REST V4 native mais
	// sont critiques pour tester la robustesse d'un intégrateur :
	// duplicate (double envoi), badSignature (kr-hash altéré),
	// raceBeforeResponse (webhook part avant le retour HTTP).
	Chaos payzen.WebhookChaos `json:"chaos,omitempty"`

	// DeliveryDelayMs retarde l'envoi du webhook (millisecondes).
	// Compose avec deux appels successifs pour simuler du out-of-order
	// sans flag dédié.
	DeliveryDelayMs int `json:"deliveryDelayMs,omitempty"`
}

// SimulatePaymentResponse retourne le deliveryId et le hash calculé.
type SimulatePaymentResponse struct {
	// DeliveryID identifie la livraison déclenchée, pour la retrouver
	// dans l'historique des webhooks.
	DeliveryID string `json:"deliveryId"`

	// KrHash est la signature réellement calculée. Retournée même
	// lorsque le chaos bad-signature altère celle qui part : le
	// marchand peut ainsi constater que ce qu'il reçoit ne correspond
	// pas, ce qui est tout l'intérêt de ce mode.
	KrHash string `json:"krHash"`

	// Channel rappelle le canal employé, browserReturn ou ipn.
	Channel string `json:"channel"`
}

// ReplayWebhookResponse retourne l'identifiant du nouveau webhook
// enqueue. L'original reste dans l'historique — le rejeu est une
// nouvelle tentative distincte.
type ReplayWebhookResponse struct {
	// NewDeliveryID identifie la nouvelle tentative. Un rejeu ne
	// remplace pas l'original : les deux coexistent dans l'historique,
	// ce qui permet de suivre chaque essai séparément.
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
//
// Provider vide → payzen par défaut (log Debug pour tracer l'implicite,
// visible en `PAYSIM_LOG_LEVEL=debug`).
func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var req CreatePaymentInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	provider := req.Provider
	if provider == "" {
		provider = "payzen"
		h.logger.Debug("api_provider_default", "path", r.URL.Path, "provider", provider)
	}
	switch provider {
	case "payzen":
		if h.payzenHandler == nil {
			http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
			return
		}
		tx, err := h.payzenHandler.Create(payzen.CreateInput{
			Amount:             req.Amount,
			Currency:           req.Currency,
			OrderID:            req.OrderID,
			FormAction:         req.FormAction,
			Customer:           req.Customer,
			Metadata:           req.Metadata,
			NotificationURL:    req.NotificationURL,
			Card:               req.Card,
			PaymentMethodToken: req.PaymentMethodToken,
		})
		if err != nil {
			// Les erreurs domain (montant, devise) et l'inconnu de moyen
			// de paiement sont fonctionnelles (400). Toute autre (store,
			// génération d'uuid) est infra et remonte en 500 avec log.
			if isDomainErr(err) || errors.Is(err, payzen.ErrPaymentMethodUnknown) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			h.logger.Error("api_create_payment_failed", "provider", provider, "err", err)
			http.Error(w, "erreur de creation", http.StatusInternalServerError)
			return
		}
		// Un paiement refusé ne rend pas de token : la carte reste
		// enregistrée — c'est ce qui permet de rejouer un impayé — mais
		// l'annoncer dans la même réponse que le refus laisserait croire
		// à un alias débitable. La collection, elle, expose le moyen
		// avec son verdict d'exploitabilité.
		out := CreatePaymentOutput{
			UUID:               tx.UUID,
			Provider:           "payzen",
			State:              string(tx.Payment.State()),
			PaymentMethodToken: tx.PaymentMethodToken,
		}
		if tx.Payment.State() == domain.StateDeclined {
			out.PaymentMethodToken = ""
		}
		writeJSON(w, http.StatusCreated, out)
	default:
		http.Error(w, fmt.Sprintf("provider %q inconnu", provider), http.StatusBadRequest)
	}
}

// CreateSubscriptionInput est le corps de POST /paysim/api/v1/subscriptions,
// endpoint générique cross-provider pour créer un abonnement. Le champ
// Provider distingue l'adaptateur (payzen par défaut). PaymentMethodToken
// doit correspondre à un moyen de paiement précédemment enregistré via
// POST /paysim/api/v1/payments avec Card.
//
// EffectDate et Rrule reprennent le vocabulaire PayZen / iCalendar
// (RFC 5545) — recopiés tels quels, cf. providers.md.
type CreateSubscriptionInput struct {
	// Provider choisit l'adaptateur, "payzen" à défaut.
	Provider string `json:"provider,omitempty"`

	// PaymentMethodToken désigne le moyen à prélever. Obligatoire :
	// un abonnement sans moyen de paiement n'a rien à débiter.
	PaymentMethodToken string `json:"paymentMethodToken"`

	// Amount est le montant d'une échéance en centimes, Currency sa
	// devise ISO 4217.
	Amount   format.Amount `json:"amount"`
	Currency string        `json:"currency"`

	// OrderID est la référence marchand de l'abonnement.
	OrderID string `json:"orderId,omitempty"`

	// EffectDate et Rrule décrivent l'échéancier. Stockés et restitués
	// tels quels, jamais interprétés : chaque échéance se déclenche
	// explicitement, aucun moteur ne tourne en fond.
	EffectDate string `json:"effectDate,omitempty"`
	Rrule      string `json:"rrule,omitempty"`

	// Metadata est recopiée sur chaque Transaction d'échéance, enrichie
	// de subscriptionId.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// SubscriptionOutput est la vue exposée d'un abonnement. Cancelled
// remonte pour que le marchand puisse le voir sans recharger.
type SubscriptionOutput struct {
	// ID est le subscriptionId assigné par Paysim, à passer aux
	// endpoints trigger-billing et cancel.
	ID string `json:"id"`

	// Provider nomme l'adaptateur ("payzen" aujourd'hui).
	Provider string `json:"provider"`

	// PaymentMethodToken désigne le moyen prélevé à chaque échéance.
	// L'abonnement ne le possède pas : révoquer ce moyen fait échouer
	// les échéances sans annuler l'abonnement.
	PaymentMethodToken string `json:"paymentMethodToken"`

	// Amount est le montant d'une échéance, en centimes entiers.
	Amount format.Amount `json:"amount"`

	// Currency au format ISO 4217.
	Currency string `json:"currency"`

	// OrderID est la référence marchand de l'abonnement.
	OrderID string `json:"orderId,omitempty"`

	// EffectDate et Rrule décrivent l'échéancier déclaré par le
	// marchand. Paysim les conserve et les restitue tels quels mais ne
	// les consomme jamais : aucun moteur ne tourne en fond, chaque
	// échéance est déclenchée explicitement par trigger-billing. Choix
	// délibéré, voir docs/subscriptions.md.
	EffectDate string `json:"effectDate,omitempty"`
	Rrule      string `json:"rrule,omitempty"`

	// Metadata est la map libre du marchand, restituée à l'identique.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Cancelled passe à true après un cancel. Les trigger-billing
	// suivants répondent alors 400 — l'annulation est définitive.
	Cancelled bool `json:"cancelled"`

	// CreatedAt en UTC.
	CreatedAt time.Time `json:"createdAt"`
}

// TriggerBillingOutput retourne l'identifiant du paiement créé par
// le trigger d'échéance. L'appelant peut ensuite GET /payments/{uuid}
// pour lire l'état complet et la trace du domaine.
type TriggerBillingOutput struct {
	// SubscriptionID est l'abonnement facturé.
	SubscriptionID string `json:"subscriptionId"`

	// PaymentUUID est la transaction créée pour cette échéance. Elle
	// porte subscriptionId dans sa metadata — c'est ce lien qui la
	// rattache à l'abonnement, sans table dédiée.
	PaymentUUID string `json:"paymentUuid"`

	// State est l'issue de l'échéance : captured, ou declined quand le
	// moyen est révoqué, expiré, ou porte un PAN de refus.
	State string `json:"state"`
}

// createSubscription traite POST /paysim/api/v1/subscriptions.
// Endpoint générique cross-provider — délègue au provider demandé.
func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) {
	var req CreateSubscriptionInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	provider := req.Provider
	if provider == "" {
		provider = "payzen"
		h.logger.Debug("api_provider_default", "path", r.URL.Path, "provider", provider)
	}
	switch provider {
	case "payzen":
		if h.payzenHandler == nil {
			http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
			return
		}
		sub, err := h.payzenHandler.CreateSubscription(payzen.CreateSubscriptionInput{
			PaymentMethodToken: req.PaymentMethodToken,
			Amount:             req.Amount,
			Currency:           req.Currency,
			OrderID:            req.OrderID,
			EffectDate:         req.EffectDate,
			Rrule:              req.Rrule,
			Metadata:           req.Metadata,
		})
		if err != nil {
			if isDomainErr(err) || errors.Is(err, payzen.ErrPaymentMethodUnknown) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			h.logger.Error("api_create_subscription_failed", "err", err)
			http.Error(w, "erreur de creation", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, subscriptionToOutput(sub, provider))
	default:
		http.Error(w, fmt.Sprintf("provider %q inconnu", provider), http.StatusBadRequest)
	}
}

// getSubscription traite GET /paysim/api/v1/subscriptions/{id}.
func (h *Handler) getSubscription(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
		return
	}
	sub, err := h.payzenHandler.SubscriptionByID(r.PathValue("id"))
	if err != nil {
		h.logger.Error("api_get_subscription_failed", "err", err)
		http.Error(w, "erreur de lecture", http.StatusInternalServerError)
		return
	}
	if sub == nil {
		http.Error(w, "abonnement inconnu", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionToOutput(sub, "payzen"))
}

// listSubscriptions traite GET /paysim/api/v1/subscriptions.
// Cross-provider par défaut ; ?provider=payzen filtre.
func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		writeJSON(w, http.StatusOK, []SubscriptionOutput{})
		return
	}
	// L'accès direct au store passe par le handler payzen — pas d'API
	// listSubscriptions publique dessus car listage cross-provider viendra
	// avec Stripe (phase 5) via un repo générique. Pour l'instant on
	// n'expose que payzen (unique provider câblé).
	subs, err := h.payzenSubscriptions()
	if err != nil {
		h.logger.Error("api_list_subscriptions_failed", "err", err)
		http.Error(w, "erreur de lecture", http.StatusInternalServerError)
		return
	}
	out := make([]SubscriptionOutput, 0, len(subs))
	for _, s := range subs {
		out = append(out, subscriptionToOutput(s, "payzen"))
	}
	writeJSON(w, http.StatusOK, out)
}

// payzenSubscriptions liste tous les abonnements PayZen via le
// SubscriptionRepository si dispo (mode SQLite) ; en mode mémoire
// aucun listing global n'existe côté payzen.Store — on retourne vide.
// Le converter recordToPayzenSub vit dans internal/providers/payzen
// mais n'est pas exposé publiquement, on reconstruit une Subscription
// depuis le SubscriptionRecord ici (structures alignées).
func (h *Handler) payzenSubscriptions() ([]*payzen.Subscription, error) {
	if h.subscriptionRepo == nil {
		return nil, nil
	}
	recs, err := h.subscriptionRepo.ByProvider("payzen")
	if err != nil {
		return nil, err
	}
	out := make([]*payzen.Subscription, 0, len(recs))
	for _, rec := range recs {
		var metadata map[string]string
		if rec.MetadataJSON != "" {
			_ = json.Unmarshal([]byte(rec.MetadataJSON), &metadata)
		}
		out = append(out, &payzen.Subscription{
			ID:                 rec.ID,
			OrderID:            rec.OrderID,
			Amount:             rec.Amount,
			Currency:           rec.Currency,
			PaymentMethodToken: rec.PaymentMethodToken,
			EffectDate:         rec.EffectDate,
			Rrule:              rec.Rrule,
			Metadata:           metadata,
			CreatedAt:          rec.CreatedAt,
			Cancelled:          rec.Cancelled,
		})
	}
	return out, nil
}

// triggerBilling traite POST /paysim/api/v1/subscriptions/{id}/trigger-billing.
// Déclenche manuellement une échéance : Paysim crée une Transaction, applique
// l'outcome selon révocation/expiration/magic PAN/magic amount, émet un
// event bus. Retour synchrone avec l'uuid du paiement créé et son état.
func (h *Handler) triggerBilling(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
		return
	}
	subID := r.PathValue("id")
	tx, err := h.payzenHandler.TriggerBilling(subID)
	if err != nil {
		switch {
		case errors.Is(err, payzen.ErrSubscriptionUnknown):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, payzen.ErrSubscriptionCancelled),
			errors.Is(err, payzen.ErrPaymentMethodUnknown):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			h.logger.Error("api_trigger_billing_failed", "id", subID, "err", err)
			http.Error(w, "erreur de trigger", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusCreated, TriggerBillingOutput{
		SubscriptionID: subID,
		PaymentUUID:    tx.UUID,
		State:          string(tx.Payment.State()),
	})
}

// cancelSubscription traite POST /paysim/api/v1/subscriptions/{id}/cancel.
// Idempotent : ID inconnu retourne 204 (l'état demandé « annulé » est
// vrai pour un ID inexistant), cohérent avec revoke sur payment methods.
func (h *Handler) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
		return
	}
	if err := h.payzenHandler.CancelSubscription(r.PathValue("id")); err != nil {
		h.logger.Error("api_cancel_subscription_failed", "err", err)
		http.Error(w, "erreur d'annulation", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// subscriptionToOutput sérialise pour l'API — miroir plus étroit du
// type payzen.Subscription, expose ce qui compte au marchand.
func subscriptionToOutput(sub *payzen.Subscription, provider string) SubscriptionOutput {
	return SubscriptionOutput{
		ID:                 sub.ID,
		Provider:           provider,
		PaymentMethodToken: sub.PaymentMethodToken,
		Amount:             sub.Amount,
		Currency:           sub.Currency,
		OrderID:            sub.OrderID,
		EffectDate:         sub.EffectDate,
		Rrule:              sub.Rrule,
		Metadata:           sub.Metadata,
		Cancelled:          sub.Cancelled,
		CreatedAt:          sub.CreatedAt,
	}
}

// PaymentMethodOutput est la vue exposée d'un moyen de paiement
// enregistré. PANFull volontairement absent — le simulateur stocke en
// clair, mais l'API l'expose sous forme masquée uniquement (comme un
// vrai PSP dans son back-office).
type PaymentMethodOutput struct {
	// Token est l'alias réutilisable — le paymentMethodToken à repasser
	// pour débiter sans formulaire. Opaque, propre à Paysim.
	Token string `json:"token"`

	// Provider nomme l'adaptateur qui a enrôlé la carte.
	Provider string `json:"provider"`

	// PANMasked est le numéro tronqué, dérivé du PAN réellement
	// enregistré. Le PAN complet n'est jamais exposé par l'API, même
	// si le simulateur le stocke en clair.
	PANMasked string `json:"panMasked"`

	// Brand est la marque, déduite du BIN quand l'enrôlement ne la
	// fournit pas.
	Brand string `json:"brand,omitempty"`

	// HolderName est le nom du porteur tel que saisi. Absent quand
	// l'enrôlement ne l'a pas transmis — un wallet n'en fournit pas.
	HolderName string `json:"holderName,omitempty"`

	// ExpiryMonth (1-12) et ExpiryYear (4 chiffres). Une carte reste
	// valide jusqu'au dernier jour de son mois d'expiration, convention
	// bancaire reprise telle quelle.
	ExpiryMonth int `json:"expiryMonth"`
	ExpiryYear  int `json:"expiryYear"`

	// Caractérisation émetteur, telle qu'enrôlée. Absentes du JSON
	// quand l'enrôlement ne les a pas fournies — l'API ne réaffirme
	// pas les défauts appliqués au rendu du kr-answer.
	Country         string `json:"country,omitempty"`
	ProductCategory string `json:"productCategory,omitempty"`
	IssuerName      string `json:"issuerName,omitempty"`

	// Revoked marque une révocation explicite par le marchand. Distinct
	// d'Usable : révoquer est une action, être inutilisable un état qui
	// peut avoir trois causes.
	Revoked bool `json:"revoked"`

	// CreatedAt est l'instant d'enrôlement, en UTC. Il n'y a pas
	// d'UpdatedAt : un moyen enregistré ne se modifie pas, il se
	// révoque et un nouveau prend le relais.
	CreatedAt time.Time `json:"createdAt"`

	// Usable dit si ce moyen peut encore produire un paiement accepté,
	// UnusableReason pourquoi il ne le peut pas. Dérivés à la lecture,
	// jamais stockés : les trois causes — révocation, expiration, PAN
	// de refus — se déduisent de ce qui est déjà là, et un champ figé
	// deviendrait faux au premier changement de mois.
	//
	// Sans eux, une carte que tout débit refusera est indistinguable
	// d'une carte valide dans la collection. Le simulateur ment alors
	// sur ses propres données, ce qui est plus coûteux qu'une absence
	// d'information.
	Usable         bool   `json:"usable"`
	UnusableReason string `json:"unusableReason,omitempty"`
}

// listPaymentMethods traite GET /paysim/api/v1/payment-methods.
// Retourne la liste cross-provider, consommée par l'interface pour
// la vue « Moyens de paiement enregistrés ». En mode mémoire, aucun
// listing global n'est possible côté payzen.Store — on renvoie vide.
func (h *Handler) listPaymentMethods(w http.ResponseWriter, _ *http.Request) {
	if h.paymentMethodRepo == nil {
		writeJSON(w, http.StatusOK, []PaymentMethodOutput{})
		return
	}
	// Aujourd'hui un seul provider (payzen) — quand Stripe arrive en
	// phase 5, on itère sur la liste des providers ou on expose un
	// filtre ?provider= comme sur payments.
	recs, err := h.paymentMethodRepo.ByProvider("payzen")
	if err != nil {
		h.logger.Error("api_list_payment_methods_failed", "err", err)
		http.Error(w, "erreur de lecture", http.StatusInternalServerError)
		return
	}
	out := make([]PaymentMethodOutput, 0, len(recs))
	for _, rec := range recs {
		out = append(out, toPaymentMethodOutput(rec))
	}
	writeJSON(w, http.StatusOK, out)
}

// toPaymentMethodOutput convertit un record en vue exposée. Construite
// à la main aux deux endroits qui en avaient besoin, la struct avait
// fini par diverger : les attributs de carte ajoutés au détail
// manquaient à la liste, si bien qu'un même moyen de paiement portait
// un porteur ou pas selon la route interrogée. Un seul convertisseur
// rend cette divergence impossible.
func toPaymentMethodOutput(rec *store.PaymentMethodRecord) PaymentMethodOutput {
	usable, reason := payzen.MethodUsability(
		rec.PANFull, rec.ExpiryMonth, rec.ExpiryYear, rec.Revoked, time.Now().UTC())
	return PaymentMethodOutput{
		Usable:         usable,
		UnusableReason: reason,
		Token:           rec.Token,
		Provider:        rec.Provider,
		PANMasked:       rec.PANMasked,
		Brand:           rec.Brand,
		HolderName:      rec.HolderName,
		Country:         rec.Country,
		ProductCategory: rec.ProductCategory,
		IssuerName:      rec.IssuerName,
		ExpiryMonth:     rec.ExpiryMonth,
		ExpiryYear:      rec.ExpiryYear,
		Revoked:         rec.Revoked,
		CreatedAt:       rec.CreatedAt,
	}
}

// getPaymentMethod traite GET /paysim/api/v1/payment-methods/{token}.
// Charge un moyen de paiement isolé sans passer par le listing — utile
// pour un bookmark UI ou une navigation directe vers l'URL du détail.
// En mode mémoire (aucun repo branché), retourne 404 par défaut : sans
// listing global côté payzen.Store, l'accès unitaire n'est pas exposé.
func (h *Handler) getPaymentMethod(w http.ResponseWriter, r *http.Request) {
	if h.paymentMethodRepo == nil {
		http.Error(w, "moyen de paiement inconnu", http.StatusNotFound)
		return
	}
	token := r.PathValue("token")
	rec, err := h.paymentMethodRepo.ByToken(token)
	if err != nil {
		h.logger.Error("api_get_payment_method_failed", "token", token, "err", err)
		http.Error(w, "erreur de lecture", http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.Error(w, "moyen de paiement inconnu", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toPaymentMethodOutput(rec))
}

// revokePaymentMethod traite POST /paysim/api/v1/payment-methods/{token}/revoke.
// Marque le moyen de paiement comme révoqué — les rejeux ultérieurs par
// ce token échoueront avec un outcome UNPAID. Idempotent : un token
// inconnu retourne 204 (l'état demandé « ce token n'est plus utilisable »
// est atteint pour un token inexistant), cohérent avec DeletePayment.
func (h *Handler) revokePaymentMethod(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
		return
	}
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "token manquant", http.StatusBadRequest)
		return
	}
	if err := h.payzenHandler.RevokeMethod(token); err != nil {
		h.logger.Error("api_revoke_method_failed", "token", token, "err", err)
		http.Error(w, "revocation impossible", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
// ResetOutput détaille ce qu'une réinitialisation a supprimé. Le
// compte par table sert à la confirmation côté interface : annoncer
// « 12 paiements, 4 moyens, 2 abonnements et 18 webhooks » dit à
// l'utilisateur ce qu'il perd, là où « Êtes-vous sûr ? » ne dit rien.
type ResetOutput struct {
	// Nombre d'entrées supprimées dans chaque collection. Zéro signifie
	// que la collection était déjà vide, pas qu'elle a été ignorée — en
	// mode mémoire, les dépôts absents laissent simplement leur compte
	// à zéro.
	Payments       int `json:"payments"`
	Subscriptions  int `json:"subscriptions"`
	PaymentMethods int `json:"paymentMethods"`
	Webhooks       int `json:"webhooks"`
}

// reset vide toutes les tables — POST /paysim/api/v1/reset.
//
// Opération et non ressource, d'où le POST : elle ne supprime pas
// « une » collection mais remet le simulateur à zéro, et rend compte
// de ce qu'elle a fait.
//
// Chaque purge est tentée même si la précédente échoue : laisser la
// base à moitié vidée sans le dire serait pire qu'un échec franc. Les
// erreurs sont journalisées et la première remonte en 500, mais le
// travail déjà fait reste fait.
//
// Les dépôts cross-provider sont nil en mode mémoire ; on retombe
// alors sur le store payzen, seul existant.
func (h *Handler) reset(w http.ResponseWriter, _ *http.Request) {
	var out ResetOutput
	var firstErr error

	note := func(op string, err error) {
		if err == nil {
			return
		}
		h.logger.Error("api_reset_failed", "op", op, "err", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	if h.paymentRepo != nil {
		n, err := h.paymentRepo.DeleteAll()
		note("payments", err)
		out.Payments = n
	} else {
		n, err := h.store.DeleteAllTransactions()
		note("payments", err)
		out.Payments = n
	}

	if h.subscriptionRepo != nil {
		n, err := h.subscriptionRepo.DeleteAll()
		note("subscriptions", err)
		out.Subscriptions = n
	}
	if h.paymentMethodRepo != nil {
		n, err := h.paymentMethodRepo.DeleteAll()
		note("paymentMethods", err)
		out.PaymentMethods = n
	}

	n, err := h.queue.PurgeWebhooks()
	note("webhooks", err)
	out.Webhooks = n

	if firstErr != nil {
		http.Error(w, "reinitialisation partielle", http.StatusInternalServerError)
		return
	}

	// Un seul événement plutôt qu'un par table : l'interface doit
	// recharger l'ensemble, pas réagir quatre fois.
	h.publisher.Publish(bus.Event{
		Type: "reset",
		At:   time.Now().UTC(),
		Data: map[string]any{
			"payments":       out.Payments,
			"subscriptions":  out.Subscriptions,
			"paymentMethods": out.PaymentMethods,
			"webhooks":       out.Webhooks,
		},
	})
	writeJSON(w, http.StatusOK, out)
}

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
		Outcome:         req.Outcome,
		CardBrand:       req.CardBrand,
		ThreeDSStatus:   req.ThreeDSStatus,
		ErrorCode:       req.ErrorCode,
		ErrorMessage:    req.ErrorMessage,
		Chaos:           req.Chaos,
		DeliveryDelayMs: req.DeliveryDelayMs,
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
		Outcome:     r.Webhook.Outcome,
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
