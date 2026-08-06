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
	"strings"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/chaos"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
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

	// Clock permet aux tests d'injecter une source de temps
	// déterministe pour la vérification d'expiration des moyens de
	// paiement enregistrés. Nil = SystemClock (production).
	Clock Clock
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

// CreateInput regroupe les paramètres de création programmatique d'un
// paiement. Utilisé à la fois par le handler HTTP natif (qui remplit tous
// les champs depuis CreatePaymentRequest) et par l'API de contrôle
// générique de Paysim (qui ne renseigne que les quatre premiers). Les
// champs de contexte optionnels — FormAction, Customer, Metadata,
// ReturnURL, NotificationURL — sont vides pour un appel générique et
// portent les valeurs marchand pour un appel natif.
//
// Card et PaymentMethodToken sont deux modes de gestion des moyens de
// paiement récurrents (4.4.5) :
//   - Card + FormAction=REGISTER_PAY : Paysim génère un
//     paymentMethodToken lors de la création et l'enregistre.
//   - PaymentMethodToken (sans Card) : rejeu one-click à partir d'un
//     moyen de paiement déjà stocké — pas de formulaire, capture
//     directe, vérification d'expiration/révocation.
// Fournir les deux en même temps est une erreur (Card ignorée, seul le
// token est utilisé — mais autant l'omettre côté appelant).
type CreateInput struct {
	Amount             format.Amount
	Currency           string
	OrderID            string
	FormAction         string
	Customer           Customer
	Metadata           map[string]string
	ReturnURL          string
	NotificationURL    string
	Card               *Card
	PaymentMethodToken string
}

// ErrPaymentMethodUnknown est retournée par Create en mode rejeu quand
// le paymentMethodToken fourni n'existe pas dans le store.
var ErrPaymentMethodUnknown = errors.New("moyen de paiement inconnu")

// clock retourne l'horloge configurée ou SystemClock par défaut. Encapsulé
// pour permettre l'injection dans les tests via HandlerConfig.Clock.
func (h *Handler) clock() Clock {
	if h.cfg.Clock == nil {
		return SystemClock{}
	}
	return h.cfg.Clock
}

// Create matérialise un paiement PayZen. Trois modes selon l'input :
//
//  1. Nominal : ni Card ni PaymentMethodToken. Crée une Transaction
//     en state=initiated qui attend un simulate ultérieur.
//  2. Card fournie : enregistre le moyen de paiement (PaymentMethod
//     stocké, token opaque généré) et l'attache à la Transaction. Le
//     state reste initiated — la capture arrive au simulate. Le
//     `formAction` PayZen (REGISTER_PAY, ASK_REGISTER_PAY, PAYMENT…)
//     est conservé comme info métadata sur la Transaction mais ne
//     conditionne pas l'enrôlement : côté simulateur, dès qu'on a
//     une CB, on la stocke — cela permet notamment aux magic PANs
//     de refus d'agir au simulate du premier paiement.
//  3. Rejeu one-click : PaymentMethodToken fourni. Charge le moyen
//     stocké, vérifie révocation → expiration → magic PAN → magic
//     amount, applique directement l'outcome (PAID ou UNPAID), émet
//     le webhook. Pas de simulate côté marchand — comportement
//     PayZen réel pour un paiement récurrent.
//
// Fournir Card ET PaymentMethodToken n'a pas de sens ; seul le token
// est pris en compte (le rejeu prime).
//
// Exportée pour que le paquet api puisse consommer cette création sans
// passer par un self-loopback HTTP.
func (h *Handler) Create(in CreateInput) (*Transaction, error) {
	if in.PaymentMethodToken != "" {
		return h.createFromToken(in)
	}
	tx, err := h.createNominal(in)
	if err != nil {
		return nil, err
	}
	if in.Card != nil {
		if err := h.enrollCard(tx, in.Card); err != nil {
			return nil, err
		}
	}
	return tx, nil
}

// createNominal est la création classique — Transaction en state
// initiated, sans moyen de paiement enregistré. Utilisée directement
// pour le flow one-shot et en interne pour l'enrôlement (le
// PaymentMethod est ensuite attaché par enrollCard).
func (h *Handler) createNominal(in CreateInput) (*Transaction, error) {
	uuid, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generation uuid: %w", err)
	}
	payment, err := domain.New(uuid, in.Amount, in.Currency)
	if err != nil {
		return nil, err
	}
	token, err := newFormToken()
	if err != nil {
		return nil, fmt.Errorf("generation formToken: %w", err)
	}
	now := h.clock().Now()
	tx := &Transaction{
		FormToken:       token,
		UUID:            uuid,
		OrderID:         in.OrderID,
		Amount:          in.Amount,
		Currency:        in.Currency,
		FormAction:      in.FormAction,
		Customer:        in.Customer,
		Metadata:        in.Metadata,
		Payment:         payment,
		ReturnURL:       in.ReturnURL,
		NotificationURL: in.NotificationURL,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := h.store.Save(tx); err != nil {
		return nil, fmt.Errorf("store Save: %w", err)
	}
	h.cfg.Publisher.Publish(bus.Event{
		Type: "payment_created",
		At:   now,
		Data: map[string]any{
			"uuid":     uuid,
			"orderId":  in.OrderID,
			"amount":   in.Amount,
			"currency": in.Currency,
		},
	})
	return tx, nil
}

// enrollCard génère un paymentMethodToken opaque à partir d'une Card,
// stocke le PaymentMethod correspondant et l'attache à la Transaction.
// Appelé après createNominal quand formAction demande l'enregistrement.
func (h *Handler) enrollCard(tx *Transaction, card *Card) error {
	token, err := newFormToken()
	if err != nil {
		return fmt.Errorf("generation payment method token: %w", err)
	}
	pm := NewPaymentMethod(token, *card, h.clock().Now())
	if err := h.store.SaveMethod(pm); err != nil {
		return fmt.Errorf("store SaveMethod: %w", err)
	}
	tx.PaymentMethodToken = pm.Token
	if err := h.store.Save(tx); err != nil {
		return fmt.Errorf("store re-Save tx apres enrollment: %w", err)
	}
	return nil
}

// createFromToken exécute un rejeu one-click. Charge le moyen de
// paiement, vérifie ses conditions d'usage, applique l'outcome décidé
// et émet le webhook si une NotificationURL est fournie. Retourne la
// Transaction résultante — son state reflète l'outcome.
func (h *Handler) createFromToken(in CreateInput) (*Transaction, error) {
	pm, err := h.store.MethodByToken(in.PaymentMethodToken)
	if err != nil {
		return nil, fmt.Errorf("lookup payment method: %w", err)
	}
	if pm == nil {
		return nil, fmt.Errorf("%w: %s", ErrPaymentMethodUnknown, in.PaymentMethodToken)
	}

	uuid, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generation uuid: %w", err)
	}
	payment, err := domain.New(uuid, in.Amount, in.Currency)
	if err != nil {
		return nil, err
	}
	formToken, err := newFormToken()
	if err != nil {
		return nil, fmt.Errorf("generation formToken: %w", err)
	}
	now := h.clock().Now()
	tx := &Transaction{
		FormToken:          formToken,
		UUID:               uuid,
		OrderID:            in.OrderID,
		Amount:             in.Amount,
		Currency:           in.Currency,
		FormAction:         in.FormAction,
		Customer:           in.Customer,
		Metadata:           in.Metadata,
		Payment:            payment,
		NotificationURL:    in.NotificationURL,
		PaymentMethodToken: pm.Token,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	outcome, reason := decideReplayOutcome(pm, in.Amount, now)
	if err := applyOutcome(tx, outcome, reason); err != nil {
		return nil, fmt.Errorf("apply outcome: %w", err)
	}
	tx.UpdatedAt = h.clock().Now()

	if err := h.store.Save(tx); err != nil {
		return nil, fmt.Errorf("store Save: %w", err)
	}

	h.cfg.Publisher.Publish(bus.Event{
		Type: "payment_created",
		At:   now,
		Data: map[string]any{
			"uuid":     uuid,
			"orderId":  in.OrderID,
			"amount":   in.Amount,
			"currency": in.Currency,
			"state":    string(payment.State()),
			"replay":   true,
		},
	})

	// Webhook : URL de la requête, ou repli sur PAYSIM_CALLBACK_URL. Un
	// rejeu récurrent est déclenché sans intervention humaine — exiger
	// une notificationUrl explicite revenait à n'émettre jamais rien
	// pour l'appelant qui compte sur la configuration globale.
	// L'échec de livraison ne fait pas échouer le rejeu lui-même.
	if target := h.callbackTarget(in.NotificationURL, tx.UUID, "replay"); target != "" && h.cfg.HMACKey != "" {
		if err := h.emitReplayWebhook(tx, pm, outcome, reason, target); err != nil {
			h.logger.Warn("replay_webhook_emit_failed",
				"uuid", tx.UUID, "err", err)
		}
	}

	return tx, nil
}

// emitReplayWebhook construit et enqueue le webhook IPN qui accompagne
// un rejeu one-click. Duplique volontairement une partie de la logique
// de simulate — les deux flows partageront un helper commun quand un
// troisième cas apparaîtra (règle de trois).
func (h *Handler) emitReplayWebhook(tx *Transaction, pm *PaymentMethod, outcome, reason, targetURL string) error {
	opts := BrowserReturnOpts{
		Outcome:      outcome,
		CardBrand:    pm.Brand,
		ErrorMessage: reason,
	}
	answer := buildKrAnswer(tx, pm, opts, "", "TEST")

	deliveryID, err := newUUID()
	if err != nil {
		return fmt.Errorf("delivery uuid: %w", err)
	}
	wh, _, err := buildDeliveryWebhook(deliveryID, targetURL,
		answer, h.cfg.HMACKey, "V4/Payment", false, 0)
	if err != nil {
		return err
	}
	return h.queue.Enqueue(wh)
}

// callbackTarget résout l'URL de notification pour un webhook émis hors
// du chemin simulate — rejeu one-click et échéance d'abonnement.
//
// Ces deux chemins sont déclenchés par une machine, pas par un humain :
// un cron qui rejoue une carte n'a personne pour lui fournir une URL au
// coup par coup. C'est précisément là que la notification compte, car
// elle est le seul moyen pour le marchand d'apprendre que l'échéance est
// passée. Se replier sur PAYSIM_CALLBACK_URL n'est donc pas « émettre à
// l'aveugle » : c'est l'URL que l'opérateur a configurée pour ça, l'exact
// équivalent du back-office d'un vrai PSP.
//
// Le warn trace où part le webhook, pour qu'une livraison inattendue
// reste explicable.
func (h *Handler) callbackTarget(explicit, uuid, origin string) string {
	if explicit != "" {
		return explicit
	}
	if h.cfg.DefaultCallbackURL != "" {
		h.logger.Warn("fallback_callback_url",
			"origin", origin, "uuid", uuid, "url", h.cfg.DefaultCallbackURL)
		return h.cfg.DefaultCallbackURL
	}
	return ""
}

// evaluateMethodOutcome inspecte les trois conditions bloquantes d'un
// moyen de paiement : révocation, expiration, PAN de test réservé aux
// refus. Retourne ("", "") si tout est OK, sinon (UNPAID, raison).
// Ordre de priorité choisi pour rendre le diagnostic déterministe et
// prévisible en cas de multiples défauts sur la même CB.
//
// Utilisée aux deux moments où un PSP réel refuse : à la présentation
// (simulate) et au rejeu récurrent (charge_token). C'est cohérent avec
// le comportement bancaire — une carte expirée est refusée dès qu'un
// paiement est tenté, pas seulement au moment du prélèvement récurrent.
func evaluateMethodOutcome(pm *PaymentMethod, now time.Time) (outcome, reason string) {
	if pm.Revoked {
		return OutcomeUnpaid, "moyen de paiement revoque"
	}
	if pm.IsExpired(now) {
		return OutcomeUnpaid, "moyen de paiement expire"
	}
	if chaos.IsDeclinedTestPAN(pm.PANFull) {
		return OutcomeUnpaid, "carte de test refusee"
	}
	return "", ""
}

// decideReplayOutcome combine les 3 conditions du moyen de paiement
// et le magic amount pour choisir l'outcome d'un rejeu one-click.
// Ordre : conditions du PM (révocation/expiration/magic PAN) puis
// magic amount, sinon PAID.
func decideReplayOutcome(pm *PaymentMethod, amount format.Amount, now time.Time) (outcome, reason string) {
	if o, r := evaluateMethodOutcome(pm, now); o != "" {
		return o, r
	}
	if magic := chaos.MagicOutcome(amount); magic != "" {
		return magic, "montant magique"
	}
	return OutcomePaid, ""
}

// createPayment traite POST /api-payment/V4/Charge/CreatePayment.
// Décode le body PayZen natif, applique la latence magique éventuelle,
// délègue la création à Create, retourne le formToken au marchand.
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

	tx, err := h.Create(CreateInput{
		Amount:             req.Amount,
		Currency:           req.Currency,
		OrderID:            req.OrderID,
		FormAction:         req.FormAction,
		Customer:           req.Customer,
		Metadata:           req.Metadata,
		ReturnURL:          req.ReturnURL,
		NotificationURL:    req.NotificationURL,
		Card:               req.Card,
		PaymentMethodToken: req.PaymentMethodToken,
	})
	if err != nil {
		if errors.Is(err, ErrPaymentMethodUnknown) {
			h.writeError(w, ErrCodePaymentMethodUnknown, err.Error())
			return
		}
		h.writeDomainError(w, err)
		return
	}
	h.writeSuccess(w, CreatePaymentAnswer{FormToken: tx.FormToken})
}

// RevokeMethod expose la révocation d'un moyen de paiement au paquet
// api. Wrapper trivial sur le Store — nécessaire parce que le champ
// store est privé et qu'on ne veut pas l'exposer directement.
func (h *Handler) RevokeMethod(token string) error {
	return h.store.RevokeMethod(token)
}

// ErrSubscriptionUnknown est retournée quand un subscriptionId ne
// correspond à aucun abonnement enregistré.
var ErrSubscriptionUnknown = errors.New("abonnement inconnu")

// ErrSubscriptionCancelled est retournée par TriggerBilling sur un
// abonnement déjà annulé — un renewal après annulation n'a pas de sens
// et signale probablement un défaut du scénario.
var ErrSubscriptionCancelled = errors.New("abonnement annule")

// CreateSubscriptionInput regroupe les paramètres de création
// programmatique d'un abonnement. Utilisé par l'endpoint générique
// et le handler HTTP natif partagent la même mécanique.
type CreateSubscriptionInput struct {
	PaymentMethodToken string
	Amount             format.Amount
	Currency           string
	OrderID            string
	EffectDate         string
	Rrule              string
	Metadata           map[string]string
}

// CreateSubscription matérialise un abonnement PayZen : vérifie que
// le paymentMethodToken existe, génère un subscriptionId, persiste
// la Subscription, publie l'event `subscription_created`. Retourne la
// Subscription complète.
//
// Exportée pour partager la logique avec l'endpoint API générique
// (POST /paysim/api/v1/subscriptions), miroir de Create() pour les
// paiements one-shot.
func (h *Handler) CreateSubscription(in CreateSubscriptionInput) (*Subscription, error) {
	if in.Amount <= 0 {
		return nil, fmt.Errorf("%w: amount doit etre strictement positif", domain.ErrInvalidAmount)
	}
	if !domain.IsCurrencyCode(in.Currency) {
		return nil, fmt.Errorf("%w: currency %q invalide", domain.ErrInvalidCurrency, in.Currency)
	}
	if in.PaymentMethodToken == "" {
		return nil, errors.New("paymentMethodToken manquant")
	}
	pm, err := h.store.MethodByToken(in.PaymentMethodToken)
	if err != nil {
		return nil, fmt.Errorf("lookup payment method: %w", err)
	}
	if pm == nil {
		return nil, fmt.Errorf("%w: %s", ErrPaymentMethodUnknown, in.PaymentMethodToken)
	}
	subID, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generation subscriptionId: %w", err)
	}
	sub := &Subscription{
		ID:                 subID,
		OrderID:            in.OrderID,
		Amount:             in.Amount,
		Currency:           in.Currency,
		PaymentMethodToken: in.PaymentMethodToken,
		EffectDate:         in.EffectDate,
		Rrule:              in.Rrule,
		Metadata:           in.Metadata,
		CreatedAt:          h.clock().Now(),
	}
	if err := h.store.SaveSubscription(sub); err != nil {
		return nil, fmt.Errorf("store SaveSubscription: %w", err)
	}
	h.cfg.Publisher.Publish(bus.Event{
		Type: "subscription_created",
		At:   sub.CreatedAt,
		Data: map[string]any{
			"subscriptionId": subID,
			"orderId":        in.OrderID,
			"amount":         in.Amount,
			"currency":       in.Currency,
		},
	})
	return sub, nil
}

// SubscriptionByID lit un abonnement, retourne nil, nil si inconnu.
// Wrapper direct sur le store — même motivation que RevokeMethod.
func (h *Handler) SubscriptionByID(id string) (*Subscription, error) {
	return h.store.SubscriptionByID(id)
}

// CancelSubscription marque un abonnement comme annulé. Idempotent
// sur ID inconnu. Un renewal ultérieur sera refusé.
func (h *Handler) CancelSubscription(id string) error {
	sub, err := h.store.SubscriptionByID(id)
	if err != nil {
		return err
	}
	if sub == nil {
		return nil // idempotent : l'état demandé « annulé » est vrai pour un ID inexistant
	}
	if sub.Cancelled {
		return nil
	}
	sub.Cancelled = true
	return h.store.SaveSubscription(sub)
}

// TriggerBilling déclenche une échéance manuelle d'un abonnement : crée
// une Transaction, applique l'outcome selon la même mécanique que le
// rejeu one-click (evaluateMethodOutcome + magic amount), émet le
// webhook, retourne la Transaction. Le lien vers la Subscription se
// fait via Metadata["subscriptionId"] sur la Transaction.
//
// L'appelant décide de la fréquence des billings : le simulateur n'a
// pas de moteur RRule qui tourne en fond (déterminisme total).
func (h *Handler) TriggerBilling(subID string) (*Transaction, error) {
	sub, err := h.store.SubscriptionByID(subID)
	if err != nil {
		return nil, fmt.Errorf("lookup subscription: %w", err)
	}
	if sub == nil {
		return nil, fmt.Errorf("%w: %s", ErrSubscriptionUnknown, subID)
	}
	if sub.Cancelled {
		return nil, fmt.Errorf("%w: %s", ErrSubscriptionCancelled, subID)
	}
	pm, err := h.store.MethodByToken(sub.PaymentMethodToken)
	if err != nil {
		return nil, fmt.Errorf("lookup payment method: %w", err)
	}
	if pm == nil {
		// Le moyen a été supprimé (edge case — les tests le forcent
		// via revoke normalement). On refuse le renewal proprement.
		return nil, fmt.Errorf("%w: %s", ErrPaymentMethodUnknown, sub.PaymentMethodToken)
	}

	uuid, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generation uuid: %w", err)
	}
	payment, err := domain.New(uuid, sub.Amount, sub.Currency)
	if err != nil {
		return nil, err
	}
	formToken, err := newFormToken()
	if err != nil {
		return nil, fmt.Errorf("generation formToken: %w", err)
	}
	now := h.clock().Now()
	// Lien Transaction ↔ Subscription : via metadata (choix Q2(a)).
	// Enrichit la metadata existante de la sub sans la remplacer.
	meta := map[string]string{"subscriptionId": subID}
	for k, v := range sub.Metadata {
		if _, exists := meta[k]; !exists {
			meta[k] = v
		}
	}
	tx := &Transaction{
		FormToken:          formToken,
		UUID:               uuid,
		OrderID:            sub.OrderID,
		Amount:             sub.Amount,
		Currency:           sub.Currency,
		Metadata:           meta,
		Payment:            payment,
		PaymentMethodToken: pm.Token,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	outcome, reason := decideReplayOutcome(pm, sub.Amount, now)
	if err := applyOutcome(tx, outcome, reason); err != nil {
		return nil, fmt.Errorf("apply outcome: %w", err)
	}
	tx.UpdatedAt = h.clock().Now()

	if err := h.store.Save(tx); err != nil {
		return nil, fmt.Errorf("store Save: %w", err)
	}
	h.cfg.Publisher.Publish(bus.Event{
		Type: "subscription_billed",
		At:   now,
		Data: map[string]any{
			"subscriptionId": subID,
			"uuid":           uuid,
			"state":          string(payment.State()),
			"amount":         sub.Amount,
		},
	})
	// Webhook d'échéance. Une Subscription ne porte pas de
	// NotificationURL propre, on se replie donc sur la configuration
	// globale — et c'est le bon comportement : un renouvellement est
	// déclenché par un ordonnanceur, jamais par quelqu'un qui pourrait
	// fournir une URL. Sans cette notification, un marchand n'a aucun
	// moyen d'apprendre qu'une échéance est passée ou a échoué, ce qui
	// rend intestable toute reprise d'impayé.
	//
	// Comme pour le rejeu, un échec de livraison ne remet pas en cause
	// l'échéance elle-même : elle a eu lieu.
	if target := h.callbackTarget("", tx.UUID, "subscription_billing"); target != "" && h.cfg.HMACKey != "" {
		if err := h.emitReplayWebhook(tx, pm, outcome, reason, target); err != nil {
			h.logger.Warn("billing_webhook_emit_failed",
				"subscriptionId", subID, "uuid", tx.UUID, "err", err)
		}
	}

	return tx, nil
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
		// Lister les valeurs acceptées : sans ça, un intégrateur qui
		// envoie CAPTURED ou AUTHORIZED (avec un Z) n'a aucun moyen de
		// deviner ce qu'on attend, et le diagnostic passe par la lecture
		// du code du simulateur.
		return "", "", fmt.Errorf("outcome %q inconnu, attendu l'un de : %s",
			opts.Outcome, strings.Join(knownOutcomes(), ", "))
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
	// Conditions du moyen de paiement (révocation / expiration /
	// magic PAN) : si la Transaction porte un PaymentMethod stocké,
	// les mêmes checks qu'au rejeu récurrent s'appliquent. Un PSP
	// réel refuse une carte expirée ou révoquée dès la présentation ;
	// on reproduit ce comportement pour rester fidèle (invariant 3).
	if tx.PaymentMethodToken != "" {
		if pm, _ := h.store.MethodByToken(tx.PaymentMethodToken); pm != nil {
			if o, r := evaluateMethodOutcome(pm, h.clock().Now()); o != "" {
				opts.Outcome = o
				if opts.ErrorMessage == "" {
					opts.ErrorMessage = r
				}
			}
		}
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

	// Le moyen enrôlé alimente le bloc cardDetails. Une lecture qui
	// échoue ne doit pas faire échouer la livraison : on repart sur la
	// carte de démonstration, en le signalant dans les logs.
	var pm *PaymentMethod
	if tx.PaymentMethodToken != "" {
		pm, err = h.store.MethodByToken(tx.PaymentMethodToken)
		if err != nil {
			h.logger.Warn("payment_method_lookup_failed",
				"uuid", tx.UUID, "token", tx.PaymentMethodToken, "err", err)
			pm = nil
		}
	}

	// serverURL vide en phase 1 (arrivera avec cmd/paysim qui saura
	// son propre PublicURL). Mode "TEST" en dur — un simulateur n'a
	// pas de "PRODUCTION".
	answer := buildKrAnswer(tx, pm, opts, "", "TEST")

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
	// Duplicate : deuxième enqueue du meme contenu de webhook mais
	// avec un nouveau deliveryID. Le marchand doit dedup sur l'UUID
	// de transaction (dans kr-answer) et non sur le deliveryID —
	// deux POST HTTP distincts arrivent avec le meme kr-hash et le
	// meme kr-answer. Sans deliveryID different, le store dedup en
	// base sur la primary key et une seule ligne survit.
	if opts.Chaos.Duplicate {
		dupID, uerr := newUUID()
		if uerr != nil {
			h.logger.Warn("chaos_duplicate_uuid_failed", "err", uerr)
		} else {
			dup, _, berr := buildDeliveryWebhook(dupID, targetURL, answer, h.cfg.HMACKey, answerType,
				opts.Chaos.BadSignature, delay)
			if berr != nil {
				h.logger.Warn("chaos_duplicate_build_failed", "err", berr)
			} else if err := h.queue.Enqueue(dup); err != nil {
				h.logger.Warn("chaos_duplicate_enqueue_failed", "err", err)
			}
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
