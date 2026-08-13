// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Runner exécute un Scenario contre un Paysim distant. Il enchaîne les
// étapes dans l'ordre, mémorise le paiement courant (uuid retourné par
// le dernier create_payment) et un cursor temporel pour scoper les
// assertions webhook à la portion écoulée du scénario.
//
// Un Runner peut être réutilisé pour plusieurs scénarios : chaque appel
// à Run réinitialise l'état interne. Il n'est pas thread-safe — un
// scénario s'exécute sur une seule goroutine, séquentiellement.
type Runner struct {
	client *Client
}

// NewRunner construit un Runner branché sur c. Le client est capturé
// par référence : partager un même Client entre plusieurs Runners est
// légitime (pool de connexions HTTP mutualisé).
func NewRunner(c *Client) *Runner {
	return &Runner{client: c}
}

// StepResult décrit le passage d'une étape : son index (1-based, même
// vocabulaire que les messages d'erreur du loader), l'action, la durée
// et l'erreur éventuelle. Une étape non exécutée (annulation via ctx,
// erreur précédente en mode strict) a Duration=0 et Err="skipped".
type StepResult struct {
	// Index est le rang de l'étape, 1-based — même vocabulaire que les
	// messages d'erreur du loader, pour qu'un rapport et une erreur de
	// chargement désignent la même chose.
	Index int

	// Action est le discriminant de l'étape jouée.
	Action string

	// Duration mesure l'étape. Zéro sur une étape non exécutée.
	Duration time.Duration

	// Err est l'échec éventuel. Les assertions y déposent une erreur
	// qui enveloppe ErrAssertion, ce qui permet à la CLI de distinguer
	// un scénario faux d'une panne d'exécution et de choisir son code
	// de retour.
	Err error
}

// Report est le résultat d'un Run. Il expose la trace complète des
// étapes et une erreur agrégée. Utile pour l'affichage étape-par-étape
// que fait la CLI, et pour un usage CI qui n'a besoin que du
// booléen de succès (Err() == nil).
type Report struct {
	// Scenario est le nom déclaré dans le YAML.
	Scenario string

	// StartedAt et EndedAt bornent l'exécution. StartedAt sert aussi de
	// curseur aux assertions webhook, qui ignorent les livraisons
	// antérieures.
	StartedAt time.Time
	EndedAt   time.Time

	// Steps porte le résultat de chaque étape, dans l'ordre. Une étape
	// en échec interrompt la suite : les étapes restantes n'y figurent
	// pas.
	Steps []StepResult
}

// Err agrège les erreurs des étapes en une seule via errors.Join.
// Retourne nil si toutes les étapes ont réussi. Chaque erreur est
// préfixée du contexte étape (« etape N (action): … ») pour rester
// diagnosticable en environnement CI sans consulter la trace complète.
func (r *Report) Err() error {
	var errs []error
	for _, s := range r.Steps {
		if s.Err != nil {
			errs = append(errs, fmt.Errorf("etape %d (%s): %w", s.Index, s.Action, s.Err))
		}
	}
	return errors.Join(errs...)
}

// Duration retourne la durée totale du scénario, StartedAt à EndedAt.
func (r *Report) Duration() time.Duration { return r.EndedAt.Sub(r.StartedAt) }

// ErrAssertion identifie une erreur d'assertion (assert_state ou
// assert_webhook qui ne matche pas l'état observé). Distinguée des
// erreurs d'exécution (HTTP down, fichier YAML invalide, action non
// supportée) pour que la CLI puisse choisir un code retour différent :
// une CI qui reçoit 1 sait qu'il s'agit d'un défaut de conformité
// (le simulé ne correspond pas au scénario), 2 signale un problème
// d'infra qui empêche même d'évaluer le scénario.
//
// Wrapping via `fmt.Errorf("%w: …", ErrAssertion, …)` dans les
// méthodes doAssert*. Compatible avec `errors.Is(joined, ErrAssertion)`
// après agrégation via `errors.Join`.
var ErrAssertion = errors.New("assertion echouee")

// Run exécute s séquentiellement contre le client HTTP. S'arrête à la
// première erreur : un scénario correspond à un cas de test, un échec
// invalide les étapes suivantes (l'état du paiement diverge de ce qui
// est asserted, les assertions suivantes seraient trompeuses).
func (r *Runner) Run(ctx context.Context, s *Scenario) *Report {
	report := &Report{
		Scenario:  s.Name,
		StartedAt: time.Now().UTC(),
	}
	st := &state{startedAt: report.StartedAt}
	for i, step := range s.Steps {
		if ctx.Err() != nil {
			report.Steps = append(report.Steps, StepResult{
				Index: i + 1, Action: step.Action, Err: ctx.Err(),
			})
			break
		}
		start := time.Now()
		err := r.exec(ctx, st, step)
		report.Steps = append(report.Steps, StepResult{
			Index:    i + 1,
			Action:   step.Action,
			Duration: time.Since(start),
			Err:      err,
		})
		if err != nil {
			break
		}
	}
	report.EndedAt = time.Now().UTC()
	return report
}

// state porte le contexte d'exécution partagé entre étapes : cursor
// temporel pour les assertions webhook, uuid du paiement courant pour
// les assertions qui n'ont pas de champ id explicite (voir D4 de la
// note de conception), et token du dernier moyen de paiement
// enregistré pour les rejeux one-click (charge_token sans token
// explicite). currentSubID mémorise le dernier abonnement
// créé pour trigger_billing/assert_subscription/cancel_subscription
// sans ID explicite.
type state struct {
	startedAt    time.Time
	currentUUID  string // dernier paiement créé, référence implicite
	currentToken string // dernier paymentMethodToken vu, pour charge_token
	currentSubID string // dernier abonnement créé, pour trigger_billing

	// Chaos actif — remis à chaque inject. Consommé par le prochain
	// simulate puis remis à zéro : « inject » a une portée d'une seule
	// étape suivante, comme un one-shot. Un scénario qui veut du chaos
	// permanent doit inject avant chaque simulate.
	pendingChaos   ChaosOpts
	pendingDelayMs int
}

// exec dispatche une étape sur son handler concret. Le contrat sortant
// est simple : nil = succès, non-nil = échec avec message diagnosticable.
func (r *Runner) exec(ctx context.Context, st *state, step Step) error {
	switch step.Action {
	case ActionCreatePayment:
		return r.doCreate(ctx, st, step.CreatePayment)
	case ActionSimulate:
		return r.doSimulate(ctx, st, step.Simulate)
	case ActionInject:
		return r.doInject(ctx, st, step.Inject)
	case ActionWait:
		return r.doWait(ctx, step.Wait)
	case ActionAdvanceTime:
		return r.doAdvanceTime(ctx, step.AdvanceTime)
	case ActionAssertWebhook:
		return r.doAssertWebhook(ctx, st, step.AssertWebhook)
	case ActionAssertState:
		return r.doAssertState(ctx, st, step.AssertState)
	case ActionChargeToken:
		return r.doChargeToken(ctx, st, step.ChargeToken)
	case ActionCreateSubscription:
		return r.doCreateSubscription(ctx, st, step.CreateSubscription)
	case ActionTriggerBilling:
		return r.doTriggerBilling(ctx, st, step.TriggerBilling)
	case ActionAssertSubscription:
		return r.doAssertSubscription(ctx, st, step.AssertSubscription)
	case ActionCancelSubscription:
		return r.doCancelSubscription(ctx, st, step.CancelSubscription)
	case ActionAssertPaymentMethod:
		return r.doAssertPaymentMethod(ctx, st, step.AssertPaymentMethod)
	case ActionAssertCustomer:
		return r.doAssertCustomer(ctx, st, step.AssertCustomer)
	default:
		return fmt.Errorf("action inconnue: %q", step.Action)
	}
}

// doCreate crée un paiement et mémorise son uuid pour les assertions
// suivantes. Un create ultérieur remplace la référence courante.
// Si le paiement retourne un paymentMethodToken (enrôlement via Card),
// il est aussi mémorisé pour les charge_token à venir.
func (r *Runner) doCreate(ctx context.Context, st *state, in *CreatePayment) error {
	got, err := r.client.CreatePayment(ctx, in)
	if err != nil {
		return err
	}
	st.currentUUID = got.UUID
	if got.PaymentMethodToken != "" {
		st.currentToken = got.PaymentMethodToken
	}
	return nil
}

// doChargeToken déclenche un rejeu one-click. Token vide → utilise le
// dernier token mémorisé (implicite comme pour l'uuid dans assert_state).
// Provider vide → payzen par défaut, cohérent avec le create_payment.
// L'uuid retourné devient le nouveau paiement courant pour les
// assertions suivantes (assert_state cible bien le rejeu).
func (r *Runner) doChargeToken(ctx context.Context, st *state, in *ChargeToken) error {
	token := in.Token
	if token == "" {
		token = st.currentToken
	}
	if token == "" {
		return errors.New("charge_token sans token : place un create_payment avec card avant, ou fournis token explicitement")
	}
	provider := in.Provider
	if provider == "" {
		provider = "payzen"
	}
	got, err := r.client.ChargeToken(ctx, provider, token, in)
	if err != nil {
		return err
	}
	st.currentUUID = got.UUID
	return nil
}

// doSimulate mappe le status domain du scénario vers l'outcome PayZen
// attendu par l'API et déclenche la simulation. Le mapping est
// délibérément conservateur : seuls les statuts sans ambiguïté sont
// couverts ; un status inconnu retourne une erreur explicite plutôt
// qu'un mapping deviné. Consomme le chaos éventuellement empilé par
// un inject précédent (portée « une étape »).
func (r *Runner) doSimulate(ctx context.Context, st *state, in *Simulate) error {
	if st.currentUUID == "" {
		return errors.New("simulate sans paiement courant : place un create_payment avant")
	}
	outcome, err := mapDomainToOutcome(in.Status)
	if err != nil {
		return err
	}
	opts := SimulateOpts{
		Chaos:           st.pendingChaos,
		DeliveryDelayMs: st.pendingDelayMs,
	}
	// Chaos one-shot : consommé, on remet à zéro pour la suite.
	st.pendingChaos = ChaosOpts{}
	st.pendingDelayMs = 0
	// Channel ipn par défaut : un scénario CI n'a pas de navigateur
	// pour recevoir le POST de retour ; l'IPN suffit à déclencher le
	// webhook côté marchand et à faire progresser la machine à états.
	if err := r.client.SimulatePayment(ctx, st.currentUUID, outcome, "ipn", opts); err != nil {
		return err
	}
	// L'alias naît de l'issue, pas de la création : c'est ici qu'un
	// enrôlement accepté produit son token, et c'est donc ici qu'il faut
	// le relire pour qu'un charge_token ultérieur le trouve. Le lire à
	// la création ne rendait rien — l'autorisation n'avait pas eu lieu.
	det, err := r.client.GetPayment(ctx, st.currentUUID)
	if err != nil {
		return err
	}
	if det.PaymentMethodToken != "" {
		st.currentToken = det.PaymentMethodToken
	}
	return nil
}

// doInject empile un mode chaos pour la prochaine étape simulate.
// Mode reconnu :
//   - "duplicate"     : webhook enqueue deux fois côté serveur
//   - "bad-signature" : kr-hash altéré, le marchand doit refuser
//   - "race"          : réponse HTTP retardée 500ms, webhook part avant
//   - "delay=NNN"     : retarde l'envoi du webhook de NNN millisecondes
//
// Un mode inconnu retourne une erreur explicite — pas de dégradation
// silencieuse (le testeur doit voir tout de suite qu'il s'est trompé).
// La portée est **une seule étape suivante** : le prochain simulate
// consomme puis reset. Pour du chaos persistant, réinjecter avant
// chaque simulate.
func (r *Runner) doInject(_ context.Context, st *state, in *Inject) error {
	switch {
	case in.Mode == "duplicate":
		st.pendingChaos.Duplicate = true
	case in.Mode == "bad-signature":
		st.pendingChaos.BadSignature = true
	case in.Mode == "race":
		st.pendingChaos.RaceBeforeResponse = true
	case strings.HasPrefix(in.Mode, "delay="):
		ms, err := strconv.Atoi(strings.TrimPrefix(in.Mode, "delay="))
		if err != nil || ms <= 0 {
			return fmt.Errorf("inject mode %q: delay invalide (attendu delay=NNN en ms)", in.Mode)
		}
		st.pendingDelayMs = ms
	default:
		return fmt.Errorf("inject mode %q inconnu (attendu duplicate|bad-signature|race|delay=NNN)", in.Mode)
	}
	return nil
}

// doWait suspend l'exécution pour Duration, annulable par ctx pour ne
// pas bloquer l'arrêt d'un scénario long.
func (r *Runner) doWait(ctx context.Context, in *Wait) error {
	select {
	case <-time.After(time.Duration(in.Duration)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// doAdvanceTime fait vieillir l'instance sans dormir. L'inverse exact de
// doWait, qui dort sans faire vieillir : l'un attend une livraison,
// l'autre franchit des jours.
func (r *Runner) doAdvanceTime(ctx context.Context, in *AdvanceTime) error {
	return r.client.AdvanceClock(ctx, time.Duration(in.Duration))
}

// doAssertWebhook compte les webhooks livrés depuis le début du
// scénario (cursor startedAt), filtre optionnellement par status, et
// compare au compteur attendu. La comparaison est stricte — un delta
// signale soit un chaos non prévu, soit un défaut de la simulation.
//
// Filtrage par uuid non fait : l'API n'expose pas le paymentId sur
// WebhookEntry aujourd'hui. Un scénario multi-paiements devra attendre
// cet enrichissement pour être asserté finement. Pour un scénario
// monoprovider mono-paiement (cas courant), le cursor temporel suffit.
func (r *Runner) doAssertWebhook(ctx context.Context, st *state, in *AssertWebhook) error {
	timeout := defaultAssertWebhookTimeout
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout)
	}
	deadline := time.Now().Add(timeout)

	// La livraison d'un webhook est asynchrone : le handler enqueue et
	// répond, le worker livre et historise ensuite. Lire une seule fois
	// revient à parier sur cet ordonnancement — en mode SQLite le pari
	// est perdu régulièrement. On attend donc d'avoir le compte attendu,
	// et on n'échoue qu'au bout du délai.
	var got int
	for {
		all, err := r.client.ListWebhooks(ctx)
		if err != nil {
			return err
		}
		got = countWebhooks(all, st.startedAt, in.Status, in.Outcome)
		if got >= in.Count || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(assertWebhookPollInterval):
		}
	}

	if got != in.Count {
		return fmt.Errorf("%w: nombre de webhooks%s: obtenu %d, veut %d",
			ErrAssertion, webhookFilterLabel(in.Status, in.Outcome), got, in.Count)
	}
	return nil
}

// webhookFilterLabel décrit les filtres actifs pour le message d'erreur.
// Sans lui, deux assertions différentes produisent le même texte et on
// ne sait pas laquelle a échoué.
func webhookFilterLabel(status, outcome string) string {
	switch {
	case status != "" && outcome != "":
		return fmt.Sprintf(" avec status=%q et outcome=%q", status, outcome)
	case status != "":
		return fmt.Sprintf(" avec status=%q", status)
	case outcome != "":
		return fmt.Sprintf(" avec outcome=%q", outcome)
	}
	return ""
}

// defaultAssertWebhookTimeout borne l'attente d'assert_webhook. Cinq
// secondes couvrent largement une livraison locale ou en cluster, y
// compris sur un runner de CI lent, sans transformer un scénario faux
// en scénario long : le cas nominal sort au premier tour de boucle.
const defaultAssertWebhookTimeout = 5 * time.Second

// assertWebhookPollInterval espace les relectures. Assez court pour que
// le cas nominal ne coûte rien de perceptible, assez long pour ne pas
// marteler l'API.
const assertWebhookPollInterval = 25 * time.Millisecond

// countWebhooks compte les livraisons postérieures au cursor, filtrées
// sur status quand il est renseigné. Extrait de doAssertWebhook pour
// que la boucle d'attente reste lisible.
func countWebhooks(all []WebhookEntry, since time.Time, status, outcome string) int {
	n := 0
	for _, w := range all {
		if w.CreatedAt.Before(since) {
			continue
		}
		if status != "" && w.Status != status {
			continue
		}
		if outcome != "" && w.Outcome != outcome {
			continue
		}
		n++
	}
	return n
}

// doAssertState lit l'état du paiement courant et compare. Erreur
// explicite si aucun create_payment n'a été fait avant.
func (r *Runner) doAssertState(ctx context.Context, st *state, in *AssertState) error {
	if st.currentUUID == "" {
		return errors.New("assert_state sans paiement courant : place un create_payment avant")
	}
	got, err := r.client.GetPayment(ctx, st.currentUUID)
	if err != nil {
		return err
	}
	if got.State != in.State {
		return fmt.Errorf("%w: etat: obtenu %q, veut %q", ErrAssertion, got.State, in.State)
	}
	// Vérifié seulement s'il est demandé : la plupart des scénarios ne
	// s'intéressent pas au motif, et exiger sa présence partout
	// obligerait à le renseigner dans des cas où il n'existe pas — un
	// abandon ou une expiration n'ont pas de code bancaire.
	if in.DeclineCode != "" && got.DeclineCode != in.DeclineCode {
		return fmt.Errorf("%w: motif de refus: obtenu %q, veut %q",
			ErrAssertion, got.DeclineCode, in.DeclineCode)
	}
	return nil
}

// doCreateSubscription crée un abonnement et mémorise son ID dans
// state.currentSubID pour les trigger_billing/assert/cancel suivants.
// Token vide → dernier token vu (miroir de charge_token).
func (r *Runner) doCreateSubscription(ctx context.Context, st *state, in *CreateSubscription) error {
	token := in.Token
	if token == "" {
		token = st.currentToken
	}
	if token == "" {
		return errors.New("create_subscription sans token : place un create_payment avec card avant, ou fournis token explicitement")
	}
	provider := in.Provider
	if provider == "" {
		provider = "payzen"
	}
	got, err := r.client.CreateSubscription(ctx, provider, token,
		in.Amount, in.Currency, in.OrderID, in.EffectDate, in.Rrule, in.Metadata)
	if err != nil {
		return err
	}
	st.currentSubID = got.ID
	return nil
}

// doTriggerBilling déclenche une échéance. Le paiement créé devient le
// currentUUID pour que les assert_state suivants ciblent ce renewal.
func (r *Runner) doTriggerBilling(ctx context.Context, st *state, in *TriggerBilling) error {
	id := in.SubscriptionID
	if id == "" {
		id = st.currentSubID
	}
	if id == "" {
		return errors.New("trigger_billing sans subscription_id : place un create_subscription avant")
	}
	got, err := r.client.TriggerBilling(ctx, id)
	if err != nil {
		return err
	}
	st.currentUUID = got.PaymentUUID
	return nil
}

// doAssertSubscription vérifie l'existence et éventuellement le
// champ Cancelled d'un abonnement. Cancelled non fourni → check
// existence uniquement.
func (r *Runner) doAssertSubscription(ctx context.Context, st *state, in *AssertSubscription) error {
	id := in.SubscriptionID
	if id == "" {
		id = st.currentSubID
	}
	if id == "" {
		return errors.New("assert_subscription sans subscription_id : place un create_subscription avant")
	}
	got, err := r.client.GetSubscription(ctx, id)
	if err != nil {
		return err
	}
	if in.Cancelled != nil && got.Cancelled != *in.Cancelled {
		return fmt.Errorf("%w: cancelled: obtenu %v, veut %v",
			ErrAssertion, got.Cancelled, *in.Cancelled)
	}
	return nil
}

// doAssertPaymentMethod vérifie les attributs du moyen enregistré.
//
// Tous les écarts sont collectés avant d'échouer, plutôt que de sortir
// au premier : quand un enrôlement perd plusieurs champs à la fois — le
// cas quand un bloc entier n'est pas propagé — les voir d'un coup dit
// bien plus qu'une découverte champ par champ à chaque relance.
func (r *Runner) doAssertPaymentMethod(ctx context.Context, st *state, in *AssertPaymentMethod) error {
	token := in.Token
	if token == "" {
		token = st.currentToken
	}
	if token == "" {
		return errors.New("assert_payment_method sans token : place un create_payment avec card avant")
	}
	got, err := r.client.GetPaymentMethod(ctx, token)
	if err != nil {
		return err
	}

	var ecarts []string
	champ := func(nom, veut, obtenu string) {
		if veut != "" && obtenu != veut {
			ecarts = append(ecarts, fmt.Sprintf("%s: obtenu %q, veut %q", nom, obtenu, veut))
		}
	}
	champ("brand", in.Brand, got.Brand)
	champ("pan_masked", in.PANMasked, got.PANMasked)
	champ("holder_name", in.HolderName, got.HolderName)
	champ("country", in.Country, got.Country)
	champ("product_category", in.ProductCategory, got.ProductCategory)
	champ("issuer_name", in.IssuerName, got.IssuerName)
	champ("unusable_reason", in.UnusableReason, got.UnusableReason)
	if in.Usable != nil && got.Usable != *in.Usable {
		ecarts = append(ecarts, fmt.Sprintf("usable: obtenu %v, veut %v", got.Usable, *in.Usable))
	}

	if len(ecarts) > 0 {
		return fmt.Errorf("%w: %s", ErrAssertion, strings.Join(ecarts, " ; "))
	}
	return nil
}

// doAssertCustomer vérifie le contexte marchand restitué par le
// paiement courant.
//
// Les écarts sont cumulés avant d'échouer, comme pour
// assert_payment_method : quand un bloc entier ne revient pas, les voir
// d'un coup dit bien plus qu'une découverte champ par champ.
func (r *Runner) doAssertCustomer(ctx context.Context, st *state, in *AssertCustomer) error {
	uuid := in.UUID
	if uuid == "" {
		uuid = st.currentUUID
	}
	if uuid == "" {
		return errors.New("assert_customer sans uuid : place un create_payment avant")
	}
	got, err := r.client.GetPayment(ctx, uuid)
	if err != nil {
		return err
	}
	obtenu := got.Customer
	if obtenu == nil {
		obtenu = &Customer{}
	}

	var ecarts []string
	champ := func(nom, veut, obt string) {
		if veut != "" && obt != veut {
			ecarts = append(ecarts, fmt.Sprintf("%s: obtenu %q, veut %q", nom, obt, veut))
		}
	}
	e := in.Expect
	champ("email", e.Email, obtenu.Email)
	champ("reference", e.Reference, obtenu.Reference)

	if e.BillingDetails != nil {
		b := obtenu.BillingDetails
		if b == nil {
			b = &BillingDetails{}
		}
		champ("billing_details.first_name", e.BillingDetails.FirstName, b.FirstName)
		champ("billing_details.last_name", e.BillingDetails.LastName, b.LastName)
		champ("billing_details.address", e.BillingDetails.Address, b.Address)
		champ("billing_details.city", e.BillingDetails.City, b.City)
		champ("billing_details.zip_code", e.BillingDetails.ZipCode, b.ZipCode)
		champ("billing_details.country", e.BillingDetails.Country, b.Country)
	}
	if e.ShippingDetails != nil {
		s := obtenu.ShippingDetails
		if s == nil {
			s = &ShippingDetails{}
		}
		champ("shipping_details.city", e.ShippingDetails.City, s.City)
		champ("shipping_details.country", e.ShippingDetails.Country, s.Country)
		champ("shipping_details.shipping_method", e.ShippingDetails.ShippingMethod, s.ShippingMethod)
		champ("shipping_details.shipping_speed", e.ShippingDetails.ShippingSpeed, s.ShippingSpeed)
		champ("shipping_details.legal_name", e.ShippingDetails.LegalName, s.LegalName)
	}
	if e.ExtraDetails != nil {
		x := obtenu.ExtraDetails
		if x == nil {
			x = &ExtraDetails{}
		}
		champ("extra_details.ip_address", e.ExtraDetails.IPAddress, x.IPAddress)
		champ("extra_details.finger_print_id", e.ExtraDetails.FingerPrintID, x.FingerPrintID)
	}

	if len(ecarts) > 0 {
		return fmt.Errorf("%w: %s", ErrAssertion, strings.Join(ecarts, " ; "))
	}
	return nil
}

// doCancelSubscription annule un abonnement. Idempotent côté serveur.
func (r *Runner) doCancelSubscription(ctx context.Context, st *state, in *CancelSubscription) error {
	id := in.SubscriptionID
	if id == "" {
		id = st.currentSubID
	}
	if id == "" {
		return errors.New("cancel_subscription sans subscription_id : place un create_subscription avant")
	}
	return r.client.CancelSubscription(ctx, id)
}

// mapDomainToOutcome traduit un état-cible du domaine (vocabulaire des
// scénarios, cf. docs/states.md) vers un outcome PayZen (vocabulaire
// SimulatePaymentRequest.Outcome). C'est le seul endroit du runner où
// deux vocabulaires se croisent — les autres actions n'utilisent que
// le vocabulaire domain, puisque l'API expose l'état sous ce nom-là.
//
// Ce mapping restera valide pour Stripe (phase 7) : ses statuts natifs
// (`succeeded`, `requires_capture`, `canceled`) sont couverts par les
// mêmes états domain. Un scénario écrit une fois s'exécute contre
// n'importe quel provider.
func mapDomainToOutcome(domainStatus string) (string, error) {
	switch domainStatus {
	case "captured":
		return "PAID", nil
	case "authorized":
		return "AUTHORISED", nil
	case "declined":
		return "UNPAID", nil
	case "expired":
		return "EXPIRED", nil
	// abandoned n'est pas un état canonique du domaine mais reste utile
	// pour scénariser un abandon utilisateur — on l'accepte comme alias.
	case "abandoned":
		return "ABANDONED", nil
	default:
		return "", fmt.Errorf("status domain %q sans mapping vers un outcome provider", domainStatus)
	}
}
