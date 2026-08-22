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
	"github.com/sprimault/paysim/internal/clock"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
)

// HandlerConfig regroupe les parametres injectes au Handler. Une seule
// struct plutot que 3 parametres positionnels dans NewHandler — plus
// lisible et extensible sans breaking change.
type HandlerConfig struct {
	// HMACKey signe le retour navigateur (kr-hash-key = sha256_hmac).
	// Vide = les endpoints de simulation retournent une erreur claire
	// au premier appel.
	HMACKey string

	// RESTPassword signe les notifications serveur a serveur
	// (kr-hash-key = password). PayZen emploie deux cles selon le
	// canal, et le SDK marchand choisit la sienne d'apres kr-hash-key ;
	// tout signer avec la meme laisserait sa branche « password »
	// inexercee jusqu'a la production.
	RESTPassword string

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

	// Autoplay joue l'acte de paiement dès la création, sans attendre
	// d'appel de simulation. Alimenté depuis PAYSIM_AUTOPLAY. Faux par
	// défaut : un paiement neuf reste `initiated` tant que personne ne
	// l'a joué, ce qui est le comportement d'un vrai PSP.
	//
	// L'issue reste décidée par les valeurs magiques — ce mode
	// automatise qui appuie sur le bouton, pas ce qui en sort.
	Autoplay bool

	// Brand est la marque Lyra attribuée au trafic arrivant par les
	// routes du protocole, qui n'en transportent aucune — chez Lyra c'est
	// l'hôte qui la désigne, et Paysim n'en a qu'un. Vide vaut
	// MarqueParDefaut. Alimentée depuis PAYSIM_PAYZEN_BRAND.
	Brand string
}

// Handler regroupe l'etat necessaire pour servir les endpoints REST V4
// de PayZen et les endpoints de controle Paysim. Construit dans
// cmd/paysim/main.go, injecte au serveur HTTP.
type Handler struct {
	store  Store
	queue  *delivery.Queue
	logger *slog.Logger
	clk    clock.Clock
	cfg    HandlerConfig
}

// NewHandler instancie un Handler PayZen. Le multiplexeur HTTP est
// obtenu ensuite via Routes() — deux étapes pour que les consommateurs
// qui ont besoin d'un *Handler concret (ex. api.Handler pour Simulate)
// puissent le récupérer sans caster un http.Handler.
func NewHandler(store Store, queue *delivery.Queue, logger *slog.Logger,
	clk clock.Clock, cfg HandlerConfig) *Handler {
	return &Handler{store: store, queue: queue, logger: logger, clk: clk, cfg: cfg}
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
			ecrireErreurEnveloppee(w, http.StatusUnauthorized,
				"authentification requise : Basic Auth absente ou incomplete")
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
			ecrireErreurEnveloppee(w, http.StatusUnauthorized,
				"authentification requise : jeton Bearer absent ou invalide")
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
// paiement récurrents :
//   - Card + FormAction=REGISTER_PAY : Paysim génère un
//     paymentMethodToken lors de la création et l'enregistre.
//   - PaymentMethodToken (sans Card) : rejeu one-click à partir d'un
//     moyen de paiement déjà stocké — pas de formulaire, capture
//     directe, vérification d'expiration/révocation.
// Fournir les deux en même temps est une erreur (Card ignorée, seul le
// token est utilisé — mais autant l'omettre côté appelant).
type CreateInput struct {
	// Amount en centimes entiers, Currency en ISO 4217. Zéro est
	// valide et désigne l'enrôlement pur.
	Amount   format.Amount
	Currency string


	// Brand désigne la marque Lyra du paiement. Vide vaut celle de
	// l'instance. Renseignée par l'API de contrôle depuis le corps JSON ;
	// les routes du protocole ne la portent pas.
	Brand string
	// OrderID est la référence de commande du marchand.
	OrderID string

	// FormAction déclare l'intention PayZen. Conservée pour
	// restitution, sans effet sur l'enrôlement.
	FormAction string

	// Customer et Metadata sont le contexte marchand, restitués tels
	// quels. Vides sur un appel générique, renseignés sur un appel
	// natif V4.
	Customer Customer
	Metadata map[string]string

	// ReturnURL et NotificationURL ciblent le retour navigateur et
	// l'IPN. Absentes, les endpoints de simulation retombent sur la
	// configuration globale.
	ReturnURL       string
	NotificationURL string

	// Card enrôle un moyen de paiement à la création.
	Card *Card

	// PaymentMethodToken bascule sur le rejeu one-click : pas de
	// formulaire, issue immédiate, webhook émis. Prend le pas sur Card
	// si les deux sont fournis — mais les fournir ensemble n'a pas de
	// sens, autant omettre Card.
	PaymentMethodToken string
}

// ErrPaymentMethodUnknown est retournée par Create en mode rejeu quand
// le paymentMethodToken fourni n'existe pas dans le store.
var ErrPaymentMethodUnknown = errors.New("moyen de paiement inconnu")

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
	// Avant toute écriture : une carte inexploitable ne doit pas laisser
	// derrière elle un paiement créé et un alias mort-né.
	if in.Card != nil {
		if err := in.Card.Validate(); err != nil {
			return nil, err
		}
	}
	tx, err := h.createNominal(in)
	if err != nil {
		return nil, err
	}
	// L'enrôlement sans paiement se tranche tout de suite : chez PayZen
	// il produit une transaction de VERIFICATION — « son montant est de
	// 1.00 EUR ou 0 EUR si l'acquéreur le supporte, son statut est soit
	// Accepté soit Refusé » — dont le seul rôle est de dire si l'alias
	// peut être créé. Personne n'attend le porteur : il n'y a pas de
	// paiement à jouer.
	//
	// Un REGISTER_PAY, lui, reste suspendu au parcours : son alias
	// naîtra du simulate, comme le paiement qui le porte.
	if tx.Card != nil && isRegisterOnly(tx) {
		// Capturée avant la vérification, qui l'écarte quand elle
		// refuse. C'est ce refus-là qu'un marchand veut voir décrit
		// avec le vrai numéro masqué.
		presentee := tx.Card
		if err := h.verifyCard(tx); err != nil {
			return nil, err
		}
		// L'autoplay ne rejoue pas une issue déjà tranchée. Le faire
		// capturerait la vérification, que Lyra ne remet jamais en
		// banque — et l'autoplay a pour rôle de jouer ce qui attendait
		// le porteur, pas de changer ce qui sort.
		//
		// La notification part quand même, avec l'issue décidée
		// ci-dessus : un marchand qui attend l'IPN de son enrôlement
		// resterait suspendu sans elle.
		if h.cfg.Autoplay {
			h.notifierVerification(tx, presentee)
		}
		return tx, nil
	}
	if h.cfg.Autoplay {
		h.autoplay(tx)
	}
	return tx, nil
}

// notifierVerification émet la notification d'une vérification à zéro
// euro, sans toucher à l'état : verifyCard l'a déjà fixé.
//
// L'issue et le motif se relisent sur la transaction plutôt que d'être
// passés en paramètre — ils viennent d'être écrits, et les faire
// voyager en double invite à ce que les deux divergent.
func (h *Handler) notifierVerification(tx *Transaction, presentee *Card) {
	outcome := OutcomeAuthorised
	if tx.Payment.State() == domain.StateDeclined {
		outcome = OutcomeUnpaid
	}
	target := h.callbackTarget(tx.NotificationURL, tx.UUID, "autoplay")
	if target == "" || h.cfg.HMACKey == "" {
		return
	}
	var pm *PaymentMethod
	if tx.PaymentMethodToken != "" {
		var err error
		if pm, err = h.store.MethodByToken(tx.PaymentMethodToken); err != nil {
			h.logger.Warn("payment_method_lookup_failed",
				"uuid", tx.UUID, "token", tx.PaymentMethodToken, "err", err)
			pm = nil
		}
	}
	if err := h.emitAutoplayWebhook(
		tx, pm, presentee, outcome, tx.DeclineMessage, chaos.DeclineReason{
			Code: tx.DeclineCode, Message: tx.DeclineMessage,
		}, target,
	); err != nil {
		h.logger.Warn("verification_webhook_emit_failed", "uuid", tx.UUID, "err", err)
	}
}

// isRegisterOnly reconnaît l'enrôlement sans paiement : aucun montant à
// débiter.
//
// C'est le montant qui trancherait, pas l'intention déclarée. Un
// formAction REGISTER accompagné d'un montant reste un débit, dont
// l'alias attend l'issue ; et à zéro centime il n'y a rien à jouer, quel
// que soit le formAction — exiger l'étiquette laisserait une carte
// présentée à zéro sans issue possible, donc sans alias, pour toujours.
func isRegisterOnly(tx *Transaction) bool {
	return tx.Amount == 0
}

// verifyCard joue la vérification d'un enrôlement sans paiement :
// accepte et enrôle, ou refuse sans laisser d'alias.
//
// Le refus est un vrai refus du paiement, visible comme tel : c'est le
// rôle que PayZen donne à sa transaction de VERIFICATION, « aider le
// marchand à comprendre, depuis son Back Office, les raisons du refus
// de la création de l'alias ». Un enrôlement qui échoue en silence ne
// lui apprendrait rien.
func (h *Handler) verifyCard(tx *Transaction) error {
	// Une vérification ne débite pas : elle contrôle que la carte est
	// utilisable, pas qu'elle est approvisionnée. Ce qui la fait échouer
	// dépend donc du motif du refus, pas du chemin emprunté.
	//
	// Une carte expirée échoue toujours — il n'y a rien à interroger. Un
	// PAN de refus n'échoue que si son motif tient au statut de la
	// carte : opposition, refus d'émetteur, opération interdite. La
	// provision insuffisante passe, et l'alias naît : c'est le seul
	// levier qui produise ensuite un abonnement dont les échéances
	// refusent pour provision, le montant d'une échéance étant imposé
	// par l'échéancier et donc indisponible comme montant magique.
	usable, reason := MethodUsability(
		tx.Card.PAN, tx.Card.ExpiryMonth, tx.Card.ExpiryYear, false, h.clk.Now())
	outcome := ""
	var decline chaos.DeclineReason
	switch {
	case !usable && reason == ReasonExpired:
		outcome = OutcomeUnpaid
	case !usable && reason == ReasonDeclinedTestPAN:
		if motif := chaos.DeclineReasonForPAN(tx.Card.PAN); chaos.RefuseUneVerification(motif) {
			outcome = OutcomeUnpaid
			decline = motif
		}
	}
	if outcome == OutcomeUnpaid {
		if err := applyOutcome(tx, outcome, reason, decline); err != nil {
			return fmt.Errorf("transition domain: %w", err)
		}
		tx.Card = nil
		tx.UpdatedAt = h.clk.Now()
		return h.store.Save(tx)
	}
	// Vérification acceptée : autorisée, jamais capturée. Chez Lyra la
	// transaction de VERIFICATION « n'est jamais remise en banque et
	// reste dans l'onglet Transactions en cours » — il y a eu une
	// demande d'autorisation, pas de mouvement de fonds.
	//
	// La laisser « initiated » ferait croire qu'on attend encore le
	// porteur, alors que la vérification a eu lieu et a réussi : c'est
	// le résultat qui manquerait à l'écran, pas une étape.
	if err := applyOutcome(tx, OutcomeAuthorised, "", chaos.DeclineReason{}); err != nil {
		return fmt.Errorf("transition domain: %w", err)
	}
	if _, err := h.enrollCardPending(tx); err != nil {
		return err
	}
	tx.UpdatedAt = h.clk.Now()
	return h.store.Save(tx)
}

// enrollCardPending enrôle la carte en attente sans juger l'issue —
// l'appelant l'a déjà fait.
func (h *Handler) enrollCardPending(tx *Transaction) (*PaymentMethod, error) {
	card := tx.Card
	tx.Card = nil
	return h.enrollCard(tx, card)
}

// autoplay joue l'acte de paiement à la place du porteur : transition
// du domaine puis notification, exactement ce que produirait un appel
// de simulation.
//
// L'issue vient des valeurs magiques, jamais d'un choix propre à ce
// mode : un moyen enregistré passe par decideReplayOutcome (révocation,
// expiration, PAN de refus, montant magique), un paiement sans carte
// n'a que le montant à consulter. Ce mode automatise qui joue, pas ce
// qui sort — sans quoi il neutraliserait les quatre leviers de
// docs/testing-cards.md dès son activation.
//
// Aucune erreur n'est remontée à l'appelant : le paiement est créé, et
// c'est le seul engagement pris par Create. Un échec ici laisse une
// trace dans les logs et le paiement en `initiated`, état qu'un appel
// de simulation explicite peut encore rattraper.
func (h *Handler) autoplay(tx *Transaction) {
	outcome, reason, decline := h.decideCardOutcome(tx)

	if err := applyOutcome(tx, outcome, reason, decline); err != nil {
		h.logger.Warn("autoplay_transition_failed",
			"uuid", tx.UUID, "outcome", outcome, "err", err)
		return
	}
	// Capturée avant l'enrôlement, qui l'écarte sur un refus. C'est
	// pourtant sur un refus qu'on en a le plus besoin : sans elle, le
	// webhook décrirait une carte de démonstration.
	presentee := tx.Card
	// L'alias naît ici, une fois l'issue connue, et pas avant.
	pm, err := h.enrollIfAccepted(tx)
	if err != nil {
		h.logger.Warn("autoplay_enroll_failed", "uuid", tx.UUID, "err", err)
	}
	tx.UpdatedAt = h.clk.Now()
	if err := h.store.Save(tx); err != nil {
		h.logger.Warn("autoplay_save_failed", "uuid", tx.UUID, "err", err)
		return
	}

	h.cfg.Publisher.Publish(bus.Event{
		Type: "payment_state_changed",
		At:   tx.UpdatedAt,
		Data: map[string]any{
			"uuid":    tx.UUID,
			"orderId": tx.OrderID,
			"state":   string(tx.Payment.State()),
			"outcome": outcome,
		},
	})

	if target := h.callbackTarget(tx.NotificationURL, tx.UUID, "autoplay"); target != "" && h.cfg.HMACKey != "" {
		if err := h.emitAutoplayWebhook(tx, pm, presentee, outcome, reason, decline, target); err != nil {
			h.logger.Warn("autoplay_webhook_emit_failed", "uuid", tx.UUID, "err", err)
		}
	}
}

// emitAutoplayWebhook diffère d'emitReplayWebhook sur un seul point :
// le moyen de paiement peut être absent, un paiement sans carte n'ayant
// pas de marque à annoncer.
func (h *Handler) emitAutoplayWebhook(
	tx *Transaction, pm *PaymentMethod, presentee *Card,
	outcome, reason string, decline chaos.DeclineReason, targetURL string,
) error {
	opts := BrowserReturnOpts{Outcome: outcome, ErrorMessage: reason, DeclineReason: decline}
	if pm != nil {
		opts.CardBrand = pm.Brand
	}
	answer := buildKrAnswer(h.clk, tx, pm, presentee, opts, "", "TEST")

	deliveryID, err := newUUID()
	if err != nil {
		return fmt.Errorf("delivery uuid: %w", err)
	}
	// Notification serveur à serveur : c'est le mot de passe d'API REST
	// qui signe, pas la clé du navigateur.
	cle, nomCle := h.signature(CanalServeur)
	wh, _, err := buildDeliveryWebhook(deliveryID, targetURL,
		answer, cle, nomCle, "V4/Payment", WebhookChaos{}, 0)
	if err != nil {
		return err
	}
	return h.queue.Enqueue(wh)
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
	payment, err := domain.New(h.clk, uuid, in.Amount, in.Currency)
	if err != nil {
		return nil, err
	}
	token, err := newFormToken()
	if err != nil {
		return nil, fmt.Errorf("generation formToken: %w", err)
	}
	now := h.clk.Now()
	tx := &Transaction{
		FormToken:       token,
		UUID:            uuid,
		Brand:           h.marque(in.Brand),
		OrderID:         in.OrderID,
		Amount:          in.Amount,
		Currency:        in.Currency,
		FormAction:      in.FormAction,
		Customer:        in.Customer,
		Metadata:        in.Metadata,
		Payment:         payment,
		// La carte attend son issue : l'alias ne naîtra qu'après une
		// autorisation acceptée, comme chez PayZen.
		Card:            in.Card,
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

// decideCardOutcome choisit l'issue d'un paiement présenté avec une
// carte, avant tout enrôlement.
//
// Travaille sur la carte et non sur un PaymentMethod : l'alias n'existe
// pas encore à ce stade, et c'est justement cette issue qui décidera
// s'il doit exister. Les conditions évaluées sont les mêmes qu'au rejeu
// — expiration, PAN de refus, montant magique — pour qu'une carte
// refusée le soit pareillement au premier paiement et au centième.
//
// Sans carte, seul le montant parle : un paiement par formulaire dont
// le porteur n'a rien saisi n'a aucune condition de moyen à vérifier.
func (h *Handler) decideCardOutcome(tx *Transaction) (outcome, reason string, decline chaos.DeclineReason) {
	if tx.Card != nil {
		if o, r, d := outcomeFromUsability(
			tx.Card.PAN, tx.Card.ExpiryMonth, tx.Card.ExpiryYear, false, h.clk.Now()); o != "" {
			return o, r, d
		}
	}
	if magic := chaos.MagicOutcome(tx.Amount); magic != "" {
		return magic, "montant magique", chaos.MagicDeclineReason(tx.Amount)
	}
	return OutcomePaid, "", chaos.DeclineReason{}
}

// conditionsDuMoyen applique les conditions bloquantes au moyen que
// porte la transaction, qu'il soit déjà enrôlé ou encore en attente.
//
// Un alias prime sur une carte en attente : les deux ne coexistent pas,
// mais l'ordre rend la lecture explicite plutôt que dépendante de
// l'invariant.
func (h *Handler) conditionsDuMoyen(tx *Transaction) (outcome, reason string, decline chaos.DeclineReason) {
	if tx.PaymentMethodToken != "" {
		pm, err := h.store.MethodByToken(tx.PaymentMethodToken)
		if err != nil || pm == nil {
			return "", "", chaos.DeclineReason{}
		}
		return evaluateMethodOutcome(pm, h.clk.Now())
	}
	if tx.Card != nil {
		return outcomeFromUsability(
			tx.Card.PAN, tx.Card.ExpiryMonth, tx.Card.ExpiryYear, false, h.clk.Now())
	}
	return "", "", chaos.DeclineReason{}
}

// enrollIfAccepted crée l'alias si — et seulement si — l'issue le
// permet.
//
// C'est la règle PayZen, écrite noir sur blanc dans son guide : «
// L'alias (token) ne sera pas créé si la demande d'autorisation ou de
// renseignement est refusée. » Un refus ne laisse donc aucun alias
// derrière lui, pas même transitoirement — il n'y a rien à masquer à
// l'affichage, puisqu'il n'y a rien.
//
// Retourne nil sans erreur quand il n'y a pas de carte en attente ou
// que l'issue est un refus : ce n'est pas un échec, c'est le
// comportement attendu.
func (h *Handler) enrollIfAccepted(tx *Transaction) (*PaymentMethod, error) {
	if tx.Card == nil {
		return nil, nil
	}
	switch tx.Payment.State() {
	case domain.StateCaptured, domain.StateAuthorized:
	default:
		// Refus, abandon, expiration : la carte n'est pas enrôlée et
		// disparaît avec la tentative.
		tx.Card = nil
		return nil, nil
	}
	card := tx.Card
	tx.Card = nil
	return h.enrollCard(tx, card)
}

// enrollCard génère un paymentMethodToken opaque à partir d'une Card,
// stocke le PaymentMethod correspondant et l'attache à la Transaction.
// Appelé après createNominal quand formAction demande l'enregistrement.
// Retourne le moyen enrôlé : l'autoplay en a besoin pour décider de
// l'issue (carte expirée, PAN de refus) et pour décrire la carte dans
// le webhook.
func (h *Handler) enrollCard(tx *Transaction, card *Card) (*PaymentMethod, error) {
	token, err := newFormToken()
	if err != nil {
		return nil, fmt.Errorf("generation payment method token: %w", err)
	}
	pm := NewPaymentMethod(token, tx.Brand, *card, tx.Customer, h.clk.Now())
	if err := h.store.SaveMethod(pm); err != nil {
		return nil, fmt.Errorf("store SaveMethod: %w", err)
	}
	tx.PaymentMethodToken = pm.Token
	if err := h.store.Save(tx); err != nil {
		return nil, fmt.Errorf("store re-Save tx apres enrollment: %w", err)
	}
	return pm, nil
}

// createFromToken exécute un rejeu one-click. Charge le moyen de
// paiement, vérifie ses conditions d'usage, applique l'outcome décidé
// et émet le webhook si une NotificationURL est fournie. Retourne la
// Transaction résultante — son state reflète l'outcome.
// customerFromAlias applique la règle PayZen du paiement par alias :
// reference, email et billingDetails viennent de l'alias, pas de la
// requête.
//
// L'alias appartient au client, et c'est lui qui fait autorité. Un
// marchand qui se tromperait de référence sur un prélèvement récurrent
// ne le verrait pas chez PayZen ; Paysim ne doit pas le lui montrer
// davantage, sous peine de valider en test une intégration qui dérive
// en production.
//
// shippingDetails et extraDetails restent ceux de la requête : une
// adresse de livraison appartient à la commande — on livre à des
// endroits différents avec la même carte — et le contexte navigateur à
// la session. PayZen ne prétend pas les écraser.
//
// Un alias enrôlé avant que le client soit capturé n'en porte aucun :
// on garde alors celui de la requête, faute de mieux. Le champ à champ
// plutôt qu'un test sur la struct entière évite qu'un alias au client
// partiel efface ce que la requête savait.
func customerFromAlias(alias, requete Customer) Customer {
	out := requete
	if alias.Reference != "" {
		out.Reference = alias.Reference
	}
	if alias.Email != "" {
		out.Email = alias.Email
	}
	if alias.BillingDetails != (BillingDetails{}) {
		out.BillingDetails = alias.BillingDetails
	}
	return out
}

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
	payment, err := domain.New(h.clk, uuid, in.Amount, in.Currency)
	if err != nil {
		return nil, err
	}
	formToken, err := newFormToken()
	if err != nil {
		return nil, fmt.Errorf("generation formToken: %w", err)
	}
	now := h.clk.Now()
	tx := &Transaction{
		FormToken:          formToken,
		UUID:               uuid,
		Brand:              h.marque(in.Brand),
		OrderID:            in.OrderID,
		Amount:             in.Amount,
		Currency:           in.Currency,
		FormAction:         in.FormAction,
		Customer:           customerFromAlias(pm.Customer, in.Customer),
		Metadata:           in.Metadata,
		Payment:            payment,
		NotificationURL:    in.NotificationURL,
		PaymentMethodToken: pm.Token,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	outcome, reason, decline := decideReplayOutcome(pm, in.Amount, now)
	if err := applyOutcome(tx, outcome, reason, decline); err != nil {
		return nil, fmt.Errorf("apply outcome: %w", err)
	}
	tx.UpdatedAt = h.clk.Now()

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
		if err := h.emitReplayWebhook(tx, pm, outcome, reason, decline, target); err != nil {
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
func (h *Handler) emitReplayWebhook(
	tx *Transaction, pm *PaymentMethod,
	outcome, reason string, decline chaos.DeclineReason, targetURL string,
) error {
	opts := BrowserReturnOpts{
		Outcome:       outcome,
		CardBrand:     pm.Brand,
		ErrorMessage:  reason,
		DeclineReason: decline,
	}
	// Rejeu sur un alias : aucune carte n'a été présentée, c'est le
	// token qui a servi. Le moyen enrôlé décrit donc tout.
	answer := buildKrAnswer(h.clk, tx, pm, nil, opts, "", "TEST")

	deliveryID, err := newUUID()
	if err != nil {
		return fmt.Errorf("delivery uuid: %w", err)
	}
	// Notification serveur à serveur : c'est le mot de passe d'API REST
	// qui signe, pas la clé du navigateur.
	cle, nomCle := h.signature(CanalServeur)
	wh, _, err := buildDeliveryWebhook(deliveryID, targetURL,
		answer, cle, nomCle, "V4/Payment", WebhookChaos{}, 0)
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

// CanalSignature distingue les deux façons dont PayZen signe, parce
// qu'il ne signe pas avec la même clé selon qui reçoit.
type CanalSignature int

const (
	// CanalNavigateur — retour POST vers le navigateur, signé avec la
	// clé HMAC de la boutique, celle que le front connaît déjà.
	CanalNavigateur CanalSignature = iota

	// CanalServeur — notification serveur à serveur, signée avec le mot
	// de passe d'API REST. Le marchand la vérifie sur son back-end, où
	// il détient cette clé-là et pas l'autre.
	CanalServeur
)

// signature rend la clé et le nom annoncé dans kr-hash-key pour un
// canal. Le SDK officiel lit ce nom pour choisir laquelle de ses deux
// clés appliquer — l'annoncer faux fait vérifier avec la mauvaise.
func (h *Handler) signature(c CanalSignature) (cle, nomCle string) {
	if c == CanalServeur {
		return h.cfg.RESTPassword, "password"
	}
	return h.cfg.HMACKey, "sha256_hmac"
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
//
// Retourne trois choses distinctes qu'il ne faut pas confondre :
// l'outcome protocolaire, un motif en clair destiné au journal, et le
// code bancaire ISO 8583 sur lequel le marchand décide de retenter. Un
// moyen révoqué ou expiré n'a pas de code bancaire — c'est Paysim qui
// refuse, pas un émetteur.
func evaluateMethodOutcome(pm *PaymentMethod, now time.Time) (outcome, reason string, decline chaos.DeclineReason) {
	return outcomeFromUsability(pm.PANFull, pm.ExpiryMonth, pm.ExpiryYear, pm.Revoked, now)
}

// outcomeFromUsability traduit le verdict d'exploitabilité en issue de
// paiement, motif en clair et code bancaire.
//
// Travaille sur les champs bruts pour servir les trois moments où la
// question se pose : la carte présentée au premier paiement, l'alias au
// rejeu, l'alias à l'échéance d'un abonnement. Le même verdict aux trois
// endroits — une carte refusée au premier débit doit l'être au centième.
//
// Seul le PAN de test porte un motif bancaire : il simule un refus venu
// de l'émetteur, là où révocation et expiration sont des verdicts
// locaux. Le motif ne suit donc que si c'est bien le PAN qui a emporté
// la décision. Le lire inconditionnellement faisait annoncer « moyen de
// paiement expire (51 provision insuffisante) » à une carte à la fois
// expirée et porteuse d'un PAN de refus : la cause disait l'un, le code
// bancaire l'autre. Un marchand qui décide de reconduire sur le code
// aurait reconduit une carte périmée.
func outcomeFromUsability(pan string, expiryMonth, expiryYear int, revoked bool, now time.Time) (outcome, reason string, decline chaos.DeclineReason) {
	usable, why := MethodUsability(pan, expiryMonth, expiryYear, revoked, now)
	if usable {
		return "", "", chaos.DeclineReason{}
	}
	if why != ReasonDeclinedTestPAN {
		return OutcomeUnpaid, why, chaos.DeclineReason{}
	}
	return OutcomeUnpaid, why, chaos.DeclineReasonForPAN(pan)
}

// MethodUsability dit si un moyen de paiement peut encore produire un
// paiement accepté, et sinon pourquoi. Travaille sur les champs bruts
// plutôt que sur un type de ce paquet, pour que l'API de contrôle
// puisse l'interroger depuis un record générique sans convertir.
//
// Source unique du verdict : la décision de paiement et la vue exposée
// s'appuient dessus. Les dupliquer les ferait diverger, et une carte
// annoncée exploitable qui refuse au premier débit est exactement le
// genre de mensonge qu'un simulateur ne doit pas produire.
//
// Le verdict est calculé à la lecture, jamais persisté : les trois
// causes se déduisent de ce qui est déjà stocké, et un champ figé
// deviendrait faux au premier changement de mois.
func MethodUsability(panFull string, expiryMonth, expiryYear int, revoked bool, now time.Time) (usable bool, reason string) {
	if revoked {
		return false, ReasonRevoked
	}
	if isExpired(expiryMonth, expiryYear, now) {
		return false, ReasonExpired
	}
	if chaos.IsDeclinedTestPAN(panFull) {
		return false, ReasonDeclinedTestPAN
	}
	return true, ""
}

// Motifs d'inexploitabilité d'un moyen de paiement, dans l'ordre où
// MethodUsability les retient.
//
// Constantes et non littéraux : le motif ne sert pas qu'à l'affichage,
// il décide si un code bancaire accompagne le refus. Une comparaison
// sur une chaîne recopiée se serait décalée à la première reformulation.
const (
	ReasonRevoked         = "moyen de paiement revoque"
	ReasonExpired         = "moyen de paiement expire"
	ReasonDeclinedTestPAN = "carte de test refusee"
)

// decideReplayOutcome combine les 3 conditions du moyen de paiement
// et le magic amount pour choisir l'outcome d'un rejeu one-click.
// Ordre : conditions du PM (révocation/expiration/magic PAN) puis
// magic amount, sinon PAID.
func decideReplayOutcome(pm *PaymentMethod, amount format.Amount, now time.Time) (outcome, reason string, decline chaos.DeclineReason) {
	if o, r, d := evaluateMethodOutcome(pm, now); o != "" {
		return o, r, d
	}
	if magic := chaos.MagicOutcome(amount); magic != "" {
		return magic, "montant magique", chaos.MagicDeclineReason(amount)
	}
	return OutcomePaid, "", chaos.DeclineReason{}
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
		if errors.Is(err, ErrInvalidCard) {
			h.writeError(w, ErrCodeInvalidCard, err.Error())
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

// ExpireMethod fait vieillir un alias jusqu'à le périmer.
//
// Une carte ne s'enrôle jamais déjà expirée — PayZen refuse
// l'autorisation, donc l'alias n'est pas créé. Elle expire après, quand
// le temps passe, et c'est ce moment-là qu'un marchand a besoin de
// reproduire : l'échéance de la semaine prochaine sur une carte qui
// vient d'atteindre sa date.
//
// Sans ce levier, le refus pour expiration ne serait plus atteignable
// autrement qu'en attendant des mois. La date posée est délibérément
// ancienne plutôt que « le mois dernier » : on cherche un état, pas une
// simulation fine du calendrier.
//
// Idempotent, comme la révocation : un token inconnu n'est pas une
// erreur, l'état demandé est déjà celui qu'on obtient.
func (h *Handler) ExpireMethod(token string) error {
	pm, err := h.store.MethodByToken(token)
	if err != nil {
		return err
	}
	if pm == nil {
		return nil
	}
	pm.ExpiryMonth = 1
	pm.ExpiryYear = 2000
	return h.store.SaveMethod(pm)
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
	// PaymentMethodToken désigne le moyen à prélever. Doit exister :
	// la création échoue sinon, plutôt que de laisser un abonnement
	// sans rien à débiter.
	PaymentMethodToken string

	// Brand désigne la marque Lyra de l'abonnement, héritée par ses
	// échéances. Vide vaut celle de l'instance.
	Brand string

	// Amount en centimes entiers, Currency en ISO 4217, OrderID libre.
	Amount   format.Amount
	Currency string
	OrderID  string

	// EffectDate et Rrule sont l'échéancier déclaré, conservé sans
	// être consommé.
	EffectDate string
	Rrule      string

	// Metadata est recopiée sur chaque Transaction d'échéance.
	Metadata map[string]string
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
		Brand:              h.marque(in.Brand),
		OrderID:            in.OrderID,
		Amount:             in.Amount,
		Currency:           in.Currency,
		PaymentMethodToken: in.PaymentMethodToken,
		EffectDate:         in.EffectDate,
		Rrule:              in.Rrule,
		Metadata:           in.Metadata,
		CreatedAt:          h.clk.Now(),
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
	payment, err := domain.New(h.clk, uuid, sub.Amount, sub.Currency)
	if err != nil {
		return nil, err
	}
	formToken, err := newFormToken()
	if err != nil {
		return nil, fmt.Errorf("generation formToken: %w", err)
	}
	now := h.clk.Now()
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
		Brand:              h.marque(sub.Brand),
		OrderID:            sub.OrderID,
		Amount:             sub.Amount,
		Currency:           sub.Currency,
		Metadata:           meta,
		Payment:            payment,
		PaymentMethodToken: pm.Token,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	outcome, reason, decline := decideReplayOutcome(pm, sub.Amount, now)
	if err := applyOutcome(tx, outcome, reason, decline); err != nil {
		return nil, fmt.Errorf("apply outcome: %w", err)
	}
	tx.UpdatedAt = h.clk.Now()

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
		if err := h.emitReplayWebhook(tx, pm, outcome, reason, decline, target); err != nil {
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
	tx.UpdatedAt = h.clk.Now()
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
	// Le token vide est tranché ici : le chemin générique le refuse aussi,
	// mais par une erreur non typée qu'on ne peut pas distinguer d'une
	// panne de dépôt. Un corps incomplet doit répondre « requête
	// invalide », jamais « erreur de stockage ».
	if req.PaymentMethodToken == "" {
		h.writeError(w, ErrCodeInvalidRequest, "paymentMethodToken manquant")
		return
	}

	// Délégué au chemin générique, comme createPayment délègue à Create.
	// L'écrire deux fois l'avait fait diverger sur deux points : la route
	// native acceptait un alias inexistant — l'abonnement se créait, puis
	// chaque échéance échouait sans que rien ne l'ait annoncé — et elle
	// n'attribuait aucune marque, si bien qu'une instance configurée sur
	// une autre marque enregistrait quand même l'abonnement sous celle
	// par défaut.
	sub, err := h.CreateSubscription(CreateSubscriptionInput{
		PaymentMethodToken: req.PaymentMethodToken,
		Amount:             req.Amount,
		Currency:           req.Currency,
		OrderID:            req.OrderID,
		EffectDate:         req.EffectDate,
		Rrule:              req.Rrule,
		Metadata:           req.Metadata,
	})
	switch {
	case err == nil:
	case errors.Is(err, ErrPaymentMethodUnknown):
		h.writeError(w, ErrCodePaymentMethodUnknown, "paymentMethodToken inconnu")
		return
	case errors.Is(err, domain.ErrInvalidAmount):
		h.writeError(w, ErrCodeInvalidAmount, "montant invalide")
		return
	case errors.Is(err, domain.ErrInvalidCurrency):
		h.writeError(w, ErrCodeInvalidCurrency, "devise invalide")
		return
	default:
		h.storeErr(w, "createSubscription.CreateSubscription", err)
		return
	}

	h.writeSuccess(w, CreateSubscriptionAnswer{SubscriptionID: sub.ID})
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
	// PayZen exige le couple : l'identifiant d'abonnement ne suffit pas.
	// Paysim l'acceptait seul, et se montrait donc plus permissif que le
	// vrai — une intégration qui omet le token passait ici et échouait en
	// production. Entre les deux erreurs possibles, celle-ci est la
	// bruyante : un appel refusé se corrige à la lecture du message, là
	// où l'acceptation muette ne se découvre qu'en prod.
	if req.PaymentMethodToken == "" {
		h.writeError(w, ErrCodeInvalidRequest, "paymentMethodToken manquant")
		return
	}
	sub, err := h.store.SubscriptionByID(req.SubscriptionID)
	if err != nil {
		h.storeErr(w, "getSubscription.SubscriptionByID", err)
		return
	}
	// Un couple incohérent est traité comme un abonnement inconnu, sans
	// distinguer les deux cas : répondre « il existe mais pas avec ce
	// moyen » renseignerait un appelant qui n'a pas le droit de le
	// savoir.
	if sub == nil || sub.PaymentMethodToken != req.PaymentMethodToken {
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
		CanalNavigateur, func(tx *Transaction) string { return tx.ReturnURL })
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
		CanalServeur, func(tx *Transaction) string { return tx.NotificationURL })
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
	// FormToken désigne la transaction à faire avancer.
	FormToken string

	// URLOverride est la cible fournie par la requête. Vide, on
	// retombe sur FallbackURL puis sur la configuration globale.
	URLOverride string

	// AnswerType est le type annoncé dans le corps signé, par exemple
	// V4/Payment. Le marchand s'en sert pour choisir son décodage.
	AnswerType string

	// Canal choisit la clé de signature et le kr-hash-key annoncé. Le
	// zéro vaut CanalNavigateur — c'est le canal d'un retour de
	// formulaire, celui qu'on joue par défaut.
	Canal CanalSignature

	// Opts porte l'issue à jouer et l'habillage du webhook.
	Opts BrowserReturnOpts

	// FallbackURL extrait l'URL par défaut de la transaction. Passée en
	// fonction parce qu'elle diffère selon le canal : le retour
	// navigateur vise ReturnURL, l'IPN vise NotificationURL.
	FallbackURL func(*Transaction) string
}

// Simulate est la logique de simulation d'un retour signé. Exportée
// pour que le paquet api puisse la consommer sans passer par un
// self-loopback HTTP. Valide, fait transiter le domain.Payment,
// construit le kr-answer, signe, enqueue le webhook via
// internal/delivery. Retourne le hash calculé et l'id de livraison,
// ou une erreur (convertie en 400 par les handlers HTTP).
func (h *Handler) Simulate(in SimulateInput) (hash, deliveryID string, err error) {
	return h.simulate(in.FormToken, in.URLOverride, in.Opts, in.AnswerType, in.Canal, in.FallbackURL)
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
	canal CanalSignature,
	fallbackURL func(*Transaction) string,
) (hash, deliveryID string, err error) {
	cle, nomCle := h.signature(canal)
	if cle == "" {
		if canal == CanalServeur {
			return "", "", errors.New("simulation impossible : PAYSIM_PAYZEN_REST_PASSWORD non configure")
		}
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
		// Le motif accompagne le refus : c'est lui qui distingue une
		// provision insuffisante d'une opposition, et donc ce qu'un
		// marchand doit faire ensuite.
		opts.DeclineReason = chaos.MagicDeclineReason(tx.Amount)
	}
	// Conditions du moyen de paiement (révocation / expiration /
	// magic PAN) : si la Transaction porte un PaymentMethod stocké,
	// les mêmes checks qu'au rejeu récurrent s'appliquent. Un PSP
	// réel refuse une carte expirée ou révoquée dès la présentation ;
	// on reproduit ce comportement pour rester fidèle (invariant 3).
	//
	// Le contrôle vaut aussi pour la carte encore en attente
	// d'enrôlement : c'est le premier paiement, celui-là même que le
	// porteur vient de jouer, et une carte périmée n'y passe pas plus
	// qu'à la centième échéance.
	if o, r, d := h.conditionsDuMoyen(tx); o != "" {
		opts.Outcome = o
		if opts.ErrorMessage == "" {
			opts.ErrorMessage = r
		}
		// Le PAN de test l'emporte sur le montant magique : il décrit
		// un refus de l'émetteur, plus spécifique.
		if d.Code != "" {
			opts.DeclineReason = d
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
	if err := applyOutcome(tx, opts.Outcome, opts.ErrorMessage, opts.DeclineReason); err != nil {
		return "", "", fmt.Errorf("transition domain: %w", err)
	}
	// L'issue est connue : la carte présentée devient un alias, ou
	// disparaît avec la tentative refusée. Le moyen est relu plus bas
	// depuis le token que l'enrôlement vient de poser.
	//
	// Capturée avant, pour que le webhook d'un refus décrive le numéro
	// réellement soumis et non une carte de démonstration.
	presentee := tx.Card
	if _, err := h.enrollIfAccepted(tx); err != nil {
		return "", "", fmt.Errorf("enrolement: %w", err)
	}
	tx.UpdatedAt = h.clk.Now()
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
	answer := buildKrAnswer(h.clk, tx, pm, presentee, opts, "", "TEST")

	deliveryID, err = newUUID()
	if err != nil {
		return "", "", fmt.Errorf("generation deliveryId: %w", err)
	}
	delay := time.Duration(opts.DeliveryDelayMs) * time.Millisecond
	wh, hash, err := buildDeliveryWebhook(deliveryID, targetURL, answer, cle, nomCle, answerType,
		opts.Chaos, delay)
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
			dup, _, berr := buildDeliveryWebhook(dupID, targetURL, answer, cle, nomCle, answerType,
				opts.Chaos, delay)
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

// ecrireErreurEnveloppee répond avec un statut HTTP donné et le corps
// enveloppé de PayZen, plutôt qu'avec du texte brut.
//
// Fonction libre et non méthode : les intercepteurs d'authentification
// s'exécutent avant qu'un Handler soit en jeu, et n'en ont pas.
//
// Un client qui décode systématiquement le JSON de l'enveloppe prenait
// une erreur de décodage là où il attendait un errorCode — le vrai
// PayZen répond en JSON structuré, y compris sur un refus
// d'authentification. Le statut HTTP, lui, reste celui du protocole :
// 401, pas 200, une authentification refusée n'étant pas une erreur
// métier.
func ecrireErreurEnveloppee(w http.ResponseWriter, statut int, message string) {
	raw, _ := json.Marshal(APIError{ErrorCode: ErrCodeUnauthorized, ErrorMessage: message})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statut)
	_ = json.NewEncoder(w).Encode(APIResponse{Status: "ERROR", Answer: raw})
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

// marque résout la marque d'un paiement : celle demandée, sinon celle de
// l'instance, sinon celle par défaut de l'adaptateur.
func (h *Handler) marque(demandee string) string {
	if demandee != "" {
		return demandee
	}
	return marqueOuDefaut(h.cfg.Brand)
}
