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
	"strings"
	"time"

	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/clock"
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

	// Clock est l'horloge que l'API pilote. Type concret et non
	// interface : une abstraction bâtie sur une seule implémentation se
	// révèle presque toujours fausse à la deuxième.
	Clock *clock.Controllable

	// Arret est fermé au début de la séquence d'arrêt pour que les
	// handlers longue durée rendent la main d'eux-mêmes. Sans lui, un
	// flux SSE ouvert retient http.Server.Shutdown jusqu'à son délai
	// complet : Shutdown ferme les connexions inactives et attend les
	// handlers actifs, sans jamais annuler leur contexte de requête.
	// Un onglet de l'interface suffisait donc à faire dépasser le délai
	// de grâce de l'orchestrateur, et le processus finissait en SIGKILL.
	//
	// Laisser ce champ à nil est licite : une lecture sur un canal nil
	// bloque pour toujours, donc le select se comporte comme avant. Les
	// tests qui n'arrêtent rien n'ont rien à fournir.
	Arret <-chan struct{}
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
	clock             *clock.Controllable
	arret             <-chan struct{}
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
		clock:             deps.Clock,
		arret:             deps.Arret,
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
	mux.HandleFunc("POST /paysim/api/v1/payment-methods/{token}/expire", h.expirePaymentMethod)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions", h.createSubscription)
	mux.HandleFunc("GET /paysim/api/v1/subscriptions", h.listSubscriptions)
	mux.HandleFunc("GET /paysim/api/v1/subscriptions/{id}", h.getSubscription)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions/{id}/trigger-billing", h.triggerBilling)
	mux.HandleFunc("POST /paysim/api/v1/subscriptions/{id}/cancel", h.cancelSubscription)
	mux.HandleFunc("GET /paysim/api/v1/clock", h.getClock)
	mux.HandleFunc("POST /paysim/api/v1/clock/advance", h.advanceClock)
	mux.HandleFunc("POST /paysim/api/v1/clock/reset", h.resetClock)
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

	// PaymentMethodToken relie le paiement au moyen enregistré : celui
	// qu'il a enrôlé, ou celui qu'il a débité en rejeu. C'est la
	// relation que PayZen porte sur la transaction, et la seule qui
	// existe — un alias n'appartient jamais à une commande.
	//
	// Vide sur un paiement one-shot sans enrôlement. Présent ici et non
	// sur le seul détail : la liste l'affiche en colonne, et c'est de là
	// qu'on ouvre la fiche du moyen.
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`

	// DeclineCode et DeclineMessage portent le motif bancaire du refus :
	// le code de retour d'autorisation ISO 8583 tel que l'acquéreur le
	// remonte, et son libellé.
	//
	// C'est ce couple qui décide de la suite chez le marchand — un 51 se
	// retente dans trois jours, un 43 impose de réclamer une autre carte.
	// Il ne circulait jusqu'ici que dans le kr-answer et dans la note de
	// l'événement, où il finissait aplati en une phrase : ni l'interface
	// ni un marchand interrogeant cette API ne pouvaient s'en servir.
	//
	// Sur le sommaire pour la même raison que le token ci-dessus : c'est
	// en balayant la liste qu'on veut distinguer un refus reconductible
	// d'un refus définitif.
	//
	// Vides quand le refus n'a pas de motif bancaire — abandon,
	// expiration — et sur tout paiement non refusé.
	DeclineCode    string `json:"declineCode,omitempty"`
	DeclineMessage string `json:"declineMessage,omitempty"`

	// WebhookCount compte les livraisons rattachées à ce paiement,
	// rejeux compris — chaque tentative en est une.
	//
	// Compté sur ce que l'historique retient réellement, donc la même
	// source que GET /webhooks?paymentUuid= : le nombre affiché
	// correspond toujours à ce qu'on trouve en ouvrant la fiche. En
	// mémoire, l'historique est un tampon circulaire ; les livraisons
	// sorties de la fenêtre ne sont comptées nulle part, mais elles ne
	// sont plus consultables non plus.
	WebhookCount int `json:"webhookCount"`

	// WebhookReplayCount est la part de rejeux dans ce total.
	//
	// Le total seul confondait la livraison qu'un paiement produit de
	// lui-même avec celles qu'on a redemandées : après deux rejeux il
	// affichait trois, sans dire combien de fois on avait renvoyé.
	//
	// Peut égaler WebhookCount quand la livraison d'origine est sortie
	// de la fenêtre retenue.
	WebhookReplayCount int `json:"webhookReplayCount"`
}

// PaymentDetail ajoute le journal d'événements.
type PaymentDetail struct {
	PaymentSummary

	// Events est le journal complet, dans l'ordre chronologique. Il
	// raconte l'histoire du paiement là où State n'en donne que le
	// dernier mot — un remboursement partiel y laisse une trace même
	// quand l'état ne change pas.
	Events []EventEntry `json:"events"`

	// Customer et Metadata sont le contexte marchand, restitué tel quel.
	// Paysim ne les interprète jamais, mais il faut pouvoir les relire :
	// jusqu'ici ils n'étaient visibles qu'en décodant le kr-answer brut,
	// ce qui rendait invisible tout champ perdu en chemin — le défaut
	// même qu'on a corrigé deux fois, sur customer.reference puis sur
	// les blocs shipping et extra.
	Customer payzen.Customer   `json:"customer,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
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

	// PaymentUUID rattache la livraison à son paiement. Vide sur les
	// entrées historisées avant l'existence du champ : elles ne sont
	// rattachables à rien rétroactivement, le corps ne portant pas
	// toujours l'identifiant.
	PaymentUUID string `json:"paymentUuid,omitempty"`

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
	// REGISTER_PAY…). Conservée et restituée.
	//
	// Ce n'est pas elle qui décide du moment de l'enrôlement, c'est le
	// montant : à zéro centime, la carte est vérifiée et l'alias rendu
	// tout de suite ; dès qu'il y a un débit, l'alias attend son issue.
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

	// Brand est la marque du moyen enrôlé (VISA, MASTERCARD, CB…),
	// livrée avec le token pour que le marchand l'enregistre du premier
	// coup. Sans elle, il ne connaît la marque qu'à l'IPN — qui, sur un
	// moyen déjà enregistré, n'a rien à réécrire : la valeur par défaut
	// posée à l'enrôlement restait affichée jusqu'au paiement récurrent
	// suivant.
	//
	// Toujours accompagnée du token, jamais seule : sans alias à
	// enregistrer, la marque n'a rien à qualifier.
	Brand string `json:"brand,omitempty"`

	// DeclineCode et DeclineMessage qualifient un refus immédiat — rejeu
	// one-click, ou autoplay actif. Vides sur tout autre état.
	//
	// Livrés ici parce que le serveur les avait déjà en main : sans eux,
	// un intégrateur qui reçoit `state: declined` doit relire le paiement
	// pour apprendre s'il peut reconduire. Le vrai PayZen renvoie ses
	// transactions complètes dès la création ; ne rendre que l'état
	// obligeait à un aller-retour que le protocole imité ne demande pas.
	DeclineCode    string `json:"declineCode,omitempty"`
	DeclineMessage string `json:"declineMessage,omitempty"`
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

	// PaymentMethodType et CardBrand décrivent le moyen annoncé dans le
	// webhook : type (défaut CARDS) et marque (défaut VISA). Ignorés dès
	// qu'un moyen enrôlé existe — ce qu'on annonce vient alors de la
	// carte réelle.
	//
	// Pas de wallet ici, contrairement aux routes du fournisseur : la
	// forme que prend un portefeuille dans un kr-answer réel n'est
	// attestée par aucune capture, et testdata/raw est vide. Tant qu'un
	// vecteur ne le confirme pas, on ne propage pas le champ sur une
	// surface de plus.
	PaymentMethodType string `json:"paymentMethodType,omitempty"`
	CardBrand         string `json:"cardBrand,omitempty"`

	// ThreeDSStatus pilote le verdict d'authentification annoncé.
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
// PSP. L'ajout de Stripe (phase 7) se fera par ajout d'un cas au switch.
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
	switch {
	// Les cinq marques Lyra passent par le même adaptateur : c'est la
	// même passerelle, seul l'hôte les distingue chez le vrai
	// fournisseur. La marque est conservée dans l'enregistrement, ce qui
	// permet à une instance d'héberger plusieurs intégrations.
	case payzen.EstMarqueLyra(provider):
		if h.payzenHandler == nil {
			http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
			return
		}
		tx, err := h.payzenHandler.Create(payzen.CreateInput{
			Brand:              req.Provider,
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
			// Les erreurs domain (montant, devise), l'inconnu de moyen de
			// paiement et la carte invalide sont fonctionnelles (400).
			// Toute autre (store, génération d'uuid) est infra et remonte
			// en 500 avec log.
			if isDomainErr(err) || errors.Is(err, payzen.ErrPaymentMethodUnknown) ||
				errors.Is(err, payzen.ErrInvalidCard) {
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
			Provider:           tx.Brand,
			State:              string(tx.Payment.State()),
			PaymentMethodToken: tx.PaymentMethodToken,
			DeclineCode:        tx.DeclineCode,
			DeclineMessage:     tx.DeclineMessage,
		}
		if tx.Payment.State() == domain.StateDeclined {
			out.PaymentMethodToken = ""
		}
		out.Brand = h.brandOf(out.PaymentMethodToken)
		writeJSON(w, http.StatusCreated, out)
	default:
		http.Error(w, fmt.Sprintf("provider %q inconnu", provider), http.StatusBadRequest)
	}
}

// brandOf retourne la marque du moyen de paiement désigné, ou la chaîne
// vide si le token est absent ou l'entrée inconnue.
//
// Lit le store du provider et non paymentMethodRepo : ce dernier est nil
// en mode mémoire, ce qui aurait réservé la marque aux déploiements
// SQLite. Le store, lui, répond dans les deux modes.
//
// Une marque manquante n'est pas une raison d'échouer une création qui a
// réussi : le paiement existe, son token est valide, seule une mention
// d'affichage se perd. L'erreur de lecture est donc journalisée, pas
// remontée.
func (h *Handler) brandOf(token string) string {
	if token == "" || h.store == nil {
		return ""
	}
	pm, err := h.store.MethodByToken(token)
	if err != nil {
		h.logger.Error("api_brand_lookup_failed", "token", token, "err", err)
		return ""
	}
	if pm == nil {
		return ""
	}
	return pm.Brand
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

	// BillingCount est le nombre d'échéances déjà produites par cet
	// abonnement, réussies comme refusées.
	//
	// Compté à la volée en balayant les métadonnées des paiements : le
	// rattachement vit là, sans table dédiée ni compteur persisté. Un
	// compteur stocké serait à maintenir à chaque échéance et pourrait
	// diverger du réel, ce qu'un simulateur ne doit pas faire — mieux
	// vaut recompter que mentir.
	BillingCount int `json:"billingCount"`

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
	switch {
	case payzen.EstMarqueLyra(provider):
		if h.payzenHandler == nil {
			http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
			return
		}
		sub, err := h.payzenHandler.CreateSubscription(payzen.CreateSubscriptionInput{
			Brand:              req.Provider,
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
		// Relu depuis le dépôt plutôt que converti depuis le type de
		// l'adaptateur : la réponse décrit alors ce qui est réellement
		// persisté, et l'API n'a pas à connaître la forme interne de
		// PayZen.
		//
		// Un abonnement qui vient d'être créé n'a pas d'échéance.
		if h.subscriptionRepo == nil {
			h.repoManquant(w, "subscriptions")
			return
		}
		rec, err := h.subscriptionRepo.ByID(sub.ID)
		if err != nil || rec == nil {
			h.logger.Error("api_create_subscription_reread_failed", "id", sub.ID, "err", err)
			http.Error(w, "erreur de lecture", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, subscriptionToOutput(rec, 0))
	default:
		http.Error(w, fmt.Sprintf("provider %q inconnu", provider), http.StatusBadRequest)
	}
}

// getSubscription traite GET /paysim/api/v1/subscriptions/{id}.
func (h *Handler) getSubscription(w http.ResponseWriter, r *http.Request) {
	if h.subscriptionRepo == nil {
		h.repoManquant(w, "subscriptions")
		return
	}
	rec, err := h.subscriptionRepo.ByID(r.PathValue("id"))
	if err != nil {
		h.logger.Error("api_get_subscription_failed", "err", err)
		http.Error(w, "erreur de lecture", http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.Error(w, "abonnement inconnu", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionToOutput(rec, h.billingCounts()[rec.ID]))
}

// repoManquant répond quand un dépôt n'est pas câblé.
//
// Un 200 avec une liste vide était la réponse d'avant : elle affirmait
// qu'aucun objet n'existait là où le serveur ne savait simplement pas
// répondre. C'est ce mensonge qui a rendu le mode mémoire inexploitable
// sans que rien ne le signale — ni log, ni code d'erreur, ni test.
//
// 501 dit la vérité : la fonctionnalité n'est pas disponible dans cette
// configuration. Le cas ne devrait plus survenir, les deux backends
// câblant leurs dépôts ; s'il revient, il sera bruyant.
func (h *Handler) repoManquant(w http.ResponseWriter, quoi string) {
	h.logger.Error("api_repo_absent", "collection", quoi)
	http.Error(w, "collection indisponible : depot "+quoi+" non configure",
		http.StatusNotImplemented)
}

// listSubscriptions traite GET /paysim/api/v1/subscriptions.
// Cross-provider par défaut ; ?provider=payzen filtre.
func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		writeJSON(w, http.StatusOK, []SubscriptionOutput{})
		return
	}
	if h.subscriptionRepo == nil {
		h.repoManquant(w, "subscriptions")
		return
	}
	// Lu directement depuis le dépôt cross-provider. Seul PayZen est
	// câblé aujourd'hui, d'où le filtre par nom ; le jour où Stripe
	// arrive, c'est cette ligne qui s'élargit — rien d'autre, puisque
	// plus rien ici ne connaît de type d'adaptateur.
	subs, err := parMarquesLyra(h.subscriptionRepo.ByProvider)
	if err != nil {
		h.logger.Error("api_list_subscriptions_failed", "err", err)
		http.Error(w, "erreur de lecture", http.StatusInternalServerError)
		return
	}
	// Même filtre que sur les paiements : « quels abonnements prélèvent
	// ce moyen ». Un alias révoqué dont il reste un abonnement actif est
	// précisément ce qu'on veut voir d'un coup d'œil.
	token := r.URL.Query().Get("paymentMethodToken")
	marque := r.URL.Query().Get("provider")
	counts := h.billingCounts()
	out := make([]SubscriptionOutput, 0, len(subs))
	for _, s := range subs {
		if token != "" && s.PaymentMethodToken != token {
			continue
		}
		if marque != "" && s.Provider != marque {
			continue
		}
		out = append(out, subscriptionToOutput(s, counts[s.ID]))
	}
	writeJSON(w, http.StatusOK, out)
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

// subscriptionToOutput sérialise un abonnement pour l'API.
//
// Part du SubscriptionRecord cross-provider, pas du type d'un
// adaptateur. L'API convertissait auparavant le générique en
// payzen.Subscription pour le reconvertir en générique : ce détour
// n'apportait rien et aurait imposé un second convertisseur le jour où
// Stripe arrive. Le stockage fournit déjà la forme neutre, autant s'en
// servir.
//
// billingCount vient de l'appelant plutôt que d'être calculé ici : il
// suppose de parcourir les paiements, et le faire par abonnement
// relirait tout le stockage à chaque ligne d'une liste.
func subscriptionToOutput(rec *store.SubscriptionRecord, billingCount int) SubscriptionOutput {
	// Métadonnées illisibles : on rend l'abonnement sans elles plutôt
	// que de faire échouer la lecture. Elles sont du contexte marchand,
	// pas une donnée dont dépend l'affichage.
	var metadata map[string]string
	if rec.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(rec.MetadataJSON), &metadata)
	}
	return SubscriptionOutput{
		ID:                 rec.ID,
		Provider:           rec.Provider,
		PaymentMethodToken: rec.PaymentMethodToken,
		Amount:             rec.Amount,
		Currency:           rec.Currency,
		OrderID:            rec.OrderID,
		EffectDate:         rec.EffectDate,
		Rrule:              rec.Rrule,
		Metadata:           metadata,
		Cancelled:          rec.Cancelled,
		BillingCount:       billingCount,
		CreatedAt:          rec.CreatedAt,
	}
}

// billingCounts compte les échéances de chaque abonnement.
//
// Un seul parcours des paiements, quel que soit le nombre
// d'abonnements : compter abonnement par abonnement relirait tout le
// stockage à chaque ligne, et une liste de cinquante abonnements ferait
// cinquante lectures complètes pour un résultat identique.
func (h *Handler) billingCounts() map[string]int {
	txs, err := h.store.AllTransactions()
	if err != nil {
		// Un décompte manquant vaut mieux qu'une liste en erreur : la
		// page reste utilisable, le compteur affiche zéro.
		h.logger.Error("api_store_failure", "op", "AllTransactions/billingCounts", "err", err)
		return nil
	}
	counts := make(map[string]int)
	for _, tx := range txs {
		if id := tx.Metadata["subscriptionId"]; id != "" {
			counts[id]++
		}
	}
	return counts
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
// Cross-provider par défaut ; ?provider=systempay filtre.
func (h *Handler) listPaymentMethods(w http.ResponseWriter, r *http.Request) {
	if h.paymentMethodRepo == nil {
		h.repoManquant(w, "payment methods")
		return
	}
	recs, err := parMarquesLyra(h.paymentMethodRepo.ByProvider)
	if err != nil {
		h.logger.Error("api_list_payment_methods_failed", "err", err)
		http.Error(w, "erreur de lecture", http.StatusInternalServerError)
		return
	}
	marque := r.URL.Query().Get("provider")
	out := make([]PaymentMethodOutput, 0, len(recs))
	for _, rec := range recs {
		if marque != "" && rec.Provider != marque {
			continue
		}
		out = append(out, toPaymentMethodOutput(rec, h.now()))
	}
	writeJSON(w, http.StatusOK, out)
}

// toPaymentMethodOutput convertit un record en vue exposée. Construite
// à la main aux deux endroits qui en avaient besoin, la struct avait
// fini par diverger : les attributs de carte ajoutés au détail
// manquaient à la liste, si bien qu'un même moyen de paiement portait
// un porteur ou pas selon la route interrogée. Un seul convertisseur
// rend cette divergence impossible.
func toPaymentMethodOutput(rec *store.PaymentMethodRecord, now time.Time) PaymentMethodOutput {
	usable, reason := payzen.MethodUsability(
		rec.PANFull, rec.ExpiryMonth, rec.ExpiryYear, rec.Revoked, now)
	return PaymentMethodOutput{
		Usable:          usable,
		UnusableReason:  reason,
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
		h.repoManquant(w, "payment methods")
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
	writeJSON(w, http.StatusOK, toPaymentMethodOutput(rec, h.now()))
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

// expirePaymentMethod traite POST /paysim/api/v1/payment-methods/{token}/expire.
//
// Fait vieillir l'alias jusqu'à sa date d'expiration : les débits
// suivants le refusent pour « moyen de paiement expire ». C'est le
// pendant de revoke pour la seconde cause d'inexploitabilité.
//
// L'endpoint existe parce qu'une carte ne peut plus être enrôlée déjà
// expirée — PayZen refuserait l'autorisation et ne créerait pas
// d'alias. Le levier de test documenté devait donc se déplacer de
// l'enrôlement vers l'alias lui-même.
//
// Action propre à Paysim, sans équivalent PayZen : elle vit dans l'API
// de contrôle, jamais dans les routes du fournisseur, pour qu'on ne
// puisse pas la prendre pour du protocole.
//
// Idempotent : un token inconnu retourne 204, comme revoke.
func (h *Handler) expirePaymentMethod(w http.ResponseWriter, r *http.Request) {
	if h.payzenHandler == nil {
		http.Error(w, "payzen handler non configure", http.StatusServiceUnavailable)
		return
	}
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "token manquant", http.StatusBadRequest)
		return
	}
	if err := h.payzenHandler.ExpireMethod(token); err != nil {
		h.logger.Error("api_expire_method_failed", "token", token, "err", err)
		http.Error(w, "expiration impossible", http.StatusInternalServerError)
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

// listPayments traite GET /paysim/api/v1/payments.
//
// Deux filtres optionnels et cumulables : ?paymentMethodToken= pour
// « qu'a-t-on débité avec cet alias » — l'enrôlement comme les débits,
// lecture inverse de PaymentSummary.PaymentMethodToken — et
// ?subscriptionId= pour « quelles échéances a produit cet abonnement ».
//
// Le second lit la métadonnée plutôt qu'une colonne : le rattachement à
// un abonnement vit là depuis l'origine, sans table dédiée. Filtrer ici
// et non côté client est ce qui rend la réponse fiable — le sommaire
// n'expose pas les métadonnées, un front ne pourrait donc pas trancher
// lui-même, et le lui faire faire supposerait de les lui envoyer toutes.
//
// Filtrage en mémoire plutôt qu'en base : le plafond de rétention borne
// déjà le nombre de transactions, et aucun index par token ou par
// abonnement n'existe dans le contrat de store. Le jour où ça pèse,
// c'est le store qu'on étend, pas ce handler.
func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	txs, err := h.store.AllTransactions()
	if err != nil {
		h.logger.Error("api_store_failure", "op", "AllTransactions", "err", err)
		http.Error(w, "store failure", http.StatusInternalServerError)
		return
	}
	token := r.URL.Query().Get("paymentMethodToken")
	subID := r.URL.Query().Get("subscriptionId")
	// ?provider= était accepté sans être appliqué : la suppression
	// l'honorait, la lecture le passait sous silence et rendait tout.
	// Un appelant qui filtre sur une marque recevait les autres sans
	// rien pour le lui dire.
	marque := r.URL.Query().Get("provider")
	// Une lecture pour toute la page : compter ligne par ligne ferait
	// autant d'allers-retours que de paiements affichés.
	counts := h.queue.WebhookCounts()
	out := make([]PaymentSummary, 0, len(txs))
	for _, tx := range txs {
		if token != "" && tx.PaymentMethodToken != token {
			continue
		}
		if subID != "" && tx.Metadata["subscriptionId"] != subID {
			continue
		}
		if marque != "" && tx.Brand != marque {
			continue
		}
		out = append(out, toPaymentSummary(tx, counts[tx.UUID]))
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
	writeJSON(w, http.StatusOK, toPaymentDetail(tx, h.queue.WebhookCounts()[tx.UUID]))
}

// deletePayment supprime un paiement précis, et avec lui ses livraisons.
// Idempotent : un UUID inconnu retourne 204 (l'état demandé est atteint :
// ce paiement n'existe pas). 500 uniquement sur erreur d'infra.
//
// Le journal du paiement part sans traitement particulier : il est
// embarqué dans son enregistrement en mémoire, et la clé étrangère de
// payment_events est en ON DELETE CASCADE côté SQLite.
//
// La cascade décide d'un champ, pas du code de retour. Un 500 sur une
// cascade échouée ferait dire à l'interface « suppression échouée »
// pendant que l'événement fait disparaître la ligne — le paiement,
// lui, est bien supprimé.
//
// Une livraison encore en file au moment de l'appel sera historisée
// après la cascade et recréera une orpheline. Rejouer la suppression
// l'emporte.
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
	// Hors du branchement ci-dessus : en production le dépôt
	// cross-provider est toujours câblé, et une cascade posée dans une
	// seule des deux branches serait soit jamais exécutée, soit jamais
	// testée.
	webhooks, errCascade := h.queue.PurgeWebhooksByPayment(uuid)
	if errCascade != nil {
		h.logger.Error("api_store_failure", "op", "cascade_webhooks",
			"err", errCascade, "uuid", uuid)
	}
	h.publisher.Publish(bus.Event{
		Type: "payment_deleted",
		At:   h.now(),
		Data: map[string]any{"uuid": uuid},
	})
	if errCascade != nil {
		writeJSON(w, http.StatusOK, DeletePaymentOutput{Webhooks: webhooks, Partial: true})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deletePayments supprime tous les paiements. Filtrable par provider
// via le query param ?provider=payzen — sans filtre, purge complète
// cross-provider.
//
// Retourne 200 avec le compteur du nombre supprimé, pour que l'UI
// puisse afficher un feedback (« 42 paiements supprimés »).
// DeletePaymentOutput rend compte d'une suppression unitaire dont la
// cascade n'a pas abouti. Absent du cas nominal, qui répond 204 sans
// corps — un intégrateur qui ignore ce type continue de fonctionner.
type DeletePaymentOutput struct {
	// Webhooks est le nombre de livraisons retirées de l'historique.
	Webhooks int `json:"webhooks"`

	// Partial signale que le paiement est bien supprimé mais que
	// certaines de ses livraisons subsistent. Rejouer la même
	// suppression les emporte : la route reste idempotente, et c'est
	// elle qui répare.
	Partial bool `json:"partial"`
}

// PurgePaymentsOutput rend compte d'une purge : les paiements
// supprimés, et les livraisons parties avec eux.
//
// Deux nombres et non un : la purge détruit désormais l'historique des
// paiements qu'elle emporte, et l'annoncer est ce qui distingue une
// purge d'une disparition inexpliquée de l'écran des webhooks.
type PurgePaymentsOutput struct {
	Deleted  int `json:"deleted"`
	Webhooks int `json:"webhooks"`

	// Partial signale des livraisons subsistantes. Contrairement à la
	// suppression unitaire, une purge partielle ne se rejoue pas — la
	// liste des paiements est vide au second appel. La sortie est
	// POST /reset.
	Partial bool `json:"partial,omitempty"`
}

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
		At:   h.now(),
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
		// cascade est appelée après la suppression. Deux formes : par
		// UUID quand le filtre restreint la purge, sur tout ce qui est
		// rattaché quand elle emporte l'ensemble. La seconde évite de
		// lire les paiements d'abord, donc la fenêtre pendant laquelle
		// un paiement créé partirait sans figurer dans la liste.
		cascade func() (int, error)
	)
	switch {
	case h.paymentRepo != nil && provider != "":
		var uuids []string
		uuids, err = h.uuidsDuProvider(provider)
		if err == nil {
			deleted, err = h.paymentRepo.DeleteByProvider(provider)
			cascade = func() (int, error) { return h.queue.PurgeWebhooksByPayment(uuids...) }
		}
	case h.paymentRepo != nil:
		deleted, err = h.paymentRepo.DeleteAll()
		cascade = h.queue.PurgeWebhooksAttached
	case provider == "" || payzen.EstMarqueLyra(provider):
		// Mode mémoire — seul l'adaptateur Lyra existe, DeleteAll s'y
		// applique. Un provider étranger est traité comme un no-op
		// (aucune entrée à supprimer chez nous).
		deleted, err = h.store.DeleteAllTransactions()
		cascade = h.queue.PurgeWebhooksAttached
	default:
		deleted = 0
	}
	if err != nil {
		h.logger.Error("api_store_failure", "op", "DeleteAll", "err", err, "provider", provider)
		http.Error(w, "store failure", http.StatusInternalServerError)
		return
	}
	out := PurgePaymentsOutput{Deleted: deleted}
	if cascade != nil {
		var errCascade error
		out.Webhooks, errCascade = cascade()
		if errCascade != nil {
			h.logger.Error("api_store_failure", "op", "cascade_webhooks",
				"err", errCascade, "provider", provider, "deleted", deleted)
			out.Partial = true
		}
	}
	h.publisher.Publish(bus.Event{
		Type: "payments_purged",
		At:   h.now(),
		Data: map[string]any{
			"provider": provider,
			"deleted":  deleted,
		},
	})
	writeJSON(w, http.StatusOK, out)
}

// uuidsDuProvider liste les paiements d'une marque avant de les
// supprimer — leurs identifiants sont le seul moyen de retrouver leurs
// livraisons ensuite.
//
// Une fenêtre subsiste entre cette lecture et la suppression : un
// paiement créé entre les deux part sans que ses livraisons soient
// cascadées. Les deux appels sont adjacents, ce qui la réduit sans la
// fermer ; la purge non filtrée, elle, ne l'a pas du tout, puisqu'elle
// n'a rien à lire.
func (h *Handler) uuidsDuProvider(provider string) ([]string, error) {
	recs, err := h.paymentRepo.ByProvider(provider)
	if err != nil {
		return nil, err
	}
	uuids := make([]string, 0, len(recs))
	for _, rec := range recs {
		uuids = append(uuids, rec.UUID)
	}
	return uuids, nil
}

// listWebhooks retourne l'historique de livraison, restreint à un
// paiement quand paymentUuid est fourni.
//
// Le filtre interroge l'historique plutôt que de trier les 200
// dernières entrées : les webhooks d'un paiement un peu ancien en sont
// déjà sortis, et un filtre local afficherait « aucun » là où la base
// en contient — un vide trompeur plutôt qu'une donnée manquante.
func (h *Handler) listWebhooks(w http.ResponseWriter, r *http.Request) {
	var records []delivery.WebhookRecord
	if uuid := r.URL.Query().Get("paymentUuid"); uuid != "" {
		records = h.queue.WebhooksByPayment(uuid, 200)
	} else {
		records = h.queue.Recent(200)
	}
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
	wh.ID = "replay-" + racineLivraison(id) + "-" + h.now().Format("150405.000000")
	wh.Replay = true
	wh.Attempts = 0
	wh.Delay = 0
	wh.CreatedAt = h.now()
	wh.LastTryAt = time.Time{}
	if err := h.queue.Enqueue(wh); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusAccepted, ReplayWebhookResponse{NewDeliveryID: wh.ID})
}

// racineLivraison rend l'identifiant de la livraison d'origine, que
// l'on parte d'elle ou d'un de ses rejeux.
//
// Sans cela, rejouer un rejeu empile les préfixes : chaque clic
// ajoutait vingt et un caractères, sans borne, et l'identifiant se
// retrouve tel quel dans l'URL de la fiche. Le geste est tout sauf
// théorique — provoquer des livraisons en double est la raison d'être
// du simulateur.
//
// L'horodatage étant toujours le dernier segment, le dernier tiret
// sépare sans ambiguïté malgré ceux de l'UUID.
func racineLivraison(id string) string {
	if !strings.HasPrefix(id, "replay-") {
		return id
	}
	sansPrefixe := strings.TrimPrefix(id, "replay-")
	dernierTiret := strings.LastIndex(sansPrefixe, "-")
	if dernierTiret <= 0 {
		return id
	}
	return sansPrefixe[:dernierTiret]
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
		Outcome:           req.Outcome,
		PaymentMethodType: req.PaymentMethodType,
		CardBrand:         req.CardBrand,
		ThreeDSStatus:     req.ThreeDSStatus,
		ErrorCode:         req.ErrorCode,
		ErrorMessage:      req.ErrorMessage,
		Chaos:             req.Chaos,
		DeliveryDelayMs:   req.DeliveryDelayMs,
	}

	input := payzen.SimulateInput{
		FormToken:  tx.FormToken,
		AnswerType: "V4/Payment",
		Opts:       opts,
	}
	// Le canal ne choisit pas que l'URL cible : il choisit aussi la clé
	// de signature. PayZen signe le retour navigateur avec la clé HMAC
	// de la boutique et la notification serveur avec le mot de passe
	// d'API REST — deux clés que le marchand détient à deux endroits.
	switch req.Channel {
	case "browserReturn":
		input.URLOverride = req.ReturnURL
		input.Canal = payzen.CanalNavigateur
		input.FallbackURL = func(tx *payzen.Transaction) string { return tx.ReturnURL }
	case "ipn":
		input.URLOverride = req.NotificationURL
		input.Canal = payzen.CanalServeur
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
		case <-h.arret:
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

// Conversion des types internes vers les DTO exposés.
//
// La frontière est volontaire : les structures internes portent des
// types du domaine et des champs propres à un adaptateur, que l'API ne
// doit pas laisser fuir. Chaque vue décide donc de ce qu'elle expose, et
// un champ ajouté ici est un engagement envers les consommateurs — pas
// un effet de bord d'un changement interne.

// toPaymentSummary réduit une transaction provider à la vue de liste.
//
// Le sommaire ne porte que ce qui s'affiche en colonne ou sert à ouvrir
// une fiche : ni le client, ni les métadonnées, ni le journal — une
// liste de plusieurs centaines d'entrées n'a pas à transporter tout
// cela. Ce qui s'y ajoute doit donc justifier sa place, comme le token
// du moyen et le motif de refus, qu'on veut lire d'un coup d'œil.
// Les compteurs sont un paramètre et non un remplissage laissé à
// l'appelant : ils sont sérialisés quoi qu'il arrive, et un zéro oublié
// se lit « aucune livraison » au lieu de « non compté ». La fiche de
// paiement les a rendus à zéro pendant plusieurs versions pour cette
// seule raison. Exigés à la construction, c'est le compilateur qui tient
// la garantie plutôt que la vigilance du prochain appelant.
func toPaymentSummary(tx *payzen.Transaction, counts store.DeliveryCounts) PaymentSummary {
	return PaymentSummary{
		UUID:               tx.UUID,
		Provider:           tx.Brand,
		OrderID:            tx.OrderID,
		Amount:             int64(tx.Amount),
		Currency:           tx.Currency,
		PaymentMethodToken: tx.PaymentMethodToken,
		State:              string(tx.Payment.State()),
		DeclineCode:        tx.DeclineCode,
		DeclineMessage:     tx.DeclineMessage,
		CreatedAt:          tx.CreatedAt,
		UpdatedAt:          tx.UpdatedAt,
		WebhookCount:       counts.Total,
		WebhookReplayCount: counts.Replays,
	}
}

// toPaymentDetail enrichit le sommaire de ce que la fiche seule montre :
// le journal d'événements, le contexte client et les métadonnées.
//
// Construit à partir de toPaymentSummary plutôt qu'en recopiant ses
// champs — les deux vues avaient divergé par le passé sur les moyens de
// paiement, un même objet portant des attributs différents selon la
// route interrogée.
func toPaymentDetail(tx *payzen.Transaction, counts store.DeliveryCounts) PaymentDetail {
	events := tx.Payment.Events()
	dto := PaymentDetail{
		PaymentSummary: toPaymentSummary(tx, counts),
		Events:         make([]EventEntry, 0, len(events)),
		Customer:       tx.Customer,
		Metadata:       tx.Metadata,
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

// toWebhookEntry réduit une livraison à sa ligne de journal.
//
// Ni les en-têtes ni le corps n'y figurent : ils pèsent lourd et ne
// servent qu'à la vue détaillée, où le marchand relit sa signature ou
// son kr-answer.
func toWebhookEntry(r delivery.WebhookRecord) WebhookEntry {
	return WebhookEntry{
		ID:          r.Webhook.ID,
		URL:         r.Webhook.URL,
		Status:      r.Status,
		Outcome:     r.Webhook.Outcome,
		PaymentUUID: r.Webhook.PaymentUUID,
		StatusCode:  r.StatusCode,
		ErrorMsg:    r.ErrorMsg,
		Attempts:    r.Webhook.Attempts,
		CreatedAt:   r.Webhook.CreatedAt,
		CompletedAt: r.CompletedAt,
	}
}

// toWebhookDetail ajoute à l'entrée ce qui a réellement été envoyé :
// en-têtes et corps, tels quels. C'est là-dessus que se vérifie une
// signature, donc rien n'y est reformaté.
func toWebhookDetail(r delivery.WebhookRecord) WebhookDetail {
	return WebhookDetail{
		WebhookEntry: toWebhookEntry(r),
		Headers:      r.Webhook.Headers,
		Body:         string(r.Webhook.Body),
	}
}

// writeJSON sérialise une réponse et pose son en-tête.
//
// L'erreur d'encodage est ignorée volontairement : elle ne survient
// qu'après l'écriture du code de statut, quand plus rien ne peut être
// signalé au client. La traiter donnerait l'illusion d'un recours qui
// n'existe pas.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
