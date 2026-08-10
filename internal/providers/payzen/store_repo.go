// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/store"
)

// RepoStore est l'impl PayZen du contrat Store — wrapper sur trois
// repositories génériques cross-provider.
//
// Il s'appelait SQLiteStore, ce qui était trompeur : il ne dépend
// d'aucun backend, seulement des trois interfaces ci-dessous. Les deux
// modes de stockage l'emploient donc à l'identique, mémoire comprise —
// une seule traduction payzen ↔ store, et aucune divergence possible
// entre les deux. Le nom disait le contraire, et c'est en partie ce qui
// a fait construire une implémentation en mémoire séparée plutôt que de
// réutiliser celle-ci. Elle a été supprimée depuis : une seule
// traduction du contrat, empruntée par les deux modes.
//
// Les trois repositories :
//   - store.PaymentRepository pour les transactions et leur journal
//   - store.SubscriptionRepository pour les abonnements récurrents
//   - store.PaymentMethodRepository pour les moyens de paiement enregistrés
//
// Les champs spécifiques PayZen (Customer, Metadata, ReturnURL,
// NotificationURL, FormAction) sont sérialisés dans les blobs JSON des
// PaymentRecord/SubscriptionRecord/PaymentMethodRecord ; les converters
// payzenToRecord / recordToPayzen (et leurs équivalents subscription
// et method) concentrent la mécanique.
//
// Tous les repos sont fournis par l'appelant (typiquement
// cmd/paysim/main.go) — ils peuvent être partagés avec d'autres
// providers et avec l'API UI cross-provider. L'appelant garde la
// propriété et la responsabilité de leur fermeture — un
// RepoStore.Close() ne les ferme pas.
type RepoStore struct {
	repo    store.PaymentRepository
	subRepo store.SubscriptionRepository
	pmRepo  store.PaymentMethodRepository
}

// providerName identifie PayZen dans la colonne payments.provider.
const providerName = "payzen"

// NewRepoStore construit un RepoStore autour des trois repositories.
func NewRepoStore(
	payments store.PaymentRepository,
	subs store.SubscriptionRepository,
	methods store.PaymentMethodRepository,
) *RepoStore {
	return &RepoStore{
		repo:    payments,
		subRepo: subs,
		pmRepo:  methods,
	}
}

// Close est un no-op — le PaymentRepository sous-jacent est possédé
// par l'appelant (main.go) qui le ferme à shutdown.
func (s *RepoStore) Close() error {
	return nil
}

// -----------------------------------------------------------------------------
// Contrat Store
// -----------------------------------------------------------------------------

// Save sérialise la Transaction PayZen vers un PaymentRecord
// générique et délègue au repository.
func (s *RepoStore) Save(tx *Transaction) error {
	rec, err := payzenToRecord(tx)
	if err != nil {
		return err
	}
	return s.repo.Save(rec)
}

// ByToken cherche par (provider=payzen, provider_ref=FormToken).
func (s *RepoStore) ByToken(token string) (*Transaction, error) {
	rec, err := s.repo.ByProviderRef(providerName, token)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	return recordToPayzen(rec)
}

// ByUUID cherche via l'UUID (indépendant du provider).
func (s *RepoStore) ByUUID(uuid string) (*Transaction, error) {
	rec, err := s.repo.ByUUID(uuid)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	// Filtre défensif : un UUID lookup pourrait matcher un autre
	// provider. Cross-provider lookup non voulu depuis un RepoStore
	// PayZen — on renvoie nil, cohérent avec l'API.
	if rec.Provider != providerName {
		return nil, nil
	}
	return recordToPayzen(rec)
}

// Len compte les paiements PayZen uniquement.
func (s *RepoStore) Len() (int, error) {
	recs, err := s.repo.ByProvider(providerName)
	if err != nil {
		return 0, err
	}
	return len(recs), nil
}

// AllTransactions retourne toutes les transactions PayZen. Ordre :
// updated_at décroissant.
func (s *RepoStore) AllTransactions() ([]*Transaction, error) {
	recs, err := s.repo.ByProvider(providerName)
	if err != nil {
		return nil, err
	}
	out := make([]*Transaction, 0, len(recs))
	for _, rec := range recs {
		tx, err := recordToPayzen(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, tx)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Abonnements — persistance SQL via subRepo (fin du stub v1 mémoire)
// -----------------------------------------------------------------------------

// SaveSubscription sérialise et délègue au repo générique.
func (s *RepoStore) SaveSubscription(sub *Subscription) error {
	if sub == nil || sub.ID == "" {
		return nil
	}
	rec, err := payzenSubToRecord(sub)
	if err != nil {
		return err
	}
	return s.subRepo.Save(rec)
}

// SubscriptionByID lit via le repo générique et désérialise.
func (s *RepoStore) SubscriptionByID(id string) (*Subscription, error) {
	rec, err := s.subRepo.ByID(id)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.Provider != providerName {
		return nil, nil
	}
	return recordToPayzenSub(rec), nil
}

// LenSubscriptions compte les abonnements PayZen uniquement.
func (s *RepoStore) LenSubscriptions() (int, error) {
	recs, err := s.subRepo.ByProvider(providerName)
	if err != nil {
		return 0, err
	}
	return len(recs), nil
}

// -----------------------------------------------------------------------------
// Moyens de paiement enregistrés — persistance SQL via pmRepo
// -----------------------------------------------------------------------------

// SaveMethod sérialise et délègue au repo générique.
func (s *RepoStore) SaveMethod(m *PaymentMethod) error {
	if m == nil || m.Token == "" {
		return nil
	}
	return s.pmRepo.Save(payzenMethodToRecord(m))
}

// MethodByToken lit via le repo et désérialise. Filtre défensif sur
// provider — un token de la table cross-provider pourrait appartenir
// à Stripe ; on renvoie nil pour rester scoped PayZen.
func (s *RepoStore) MethodByToken(token string) (*PaymentMethod, error) {
	rec, err := s.pmRepo.ByToken(token)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.Provider != providerName {
		return nil, nil
	}
	return recordToPayzenMethod(rec), nil
}

// RevokeMethod délègue au repo. Idempotent, cf. contrat.
func (s *RepoStore) RevokeMethod(token string) error {
	return s.pmRepo.Revoke(token)
}

// Delete supprime une transaction PayZen. Le repo cross-provider est
// scoped par UUID unique, aucune ambiguïté possible.
func (s *RepoStore) Delete(uuid string) error {
	return s.repo.DeleteByUUID(uuid)
}

// DeleteAllTransactions supprime toutes les transactions PayZen —
// délègue au repo générique avec le filtre provider.
func (s *RepoStore) DeleteAllTransactions() (int, error) {
	return s.repo.DeleteByProvider(providerName)
}

// -----------------------------------------------------------------------------
// Converters PayZen ⇄ PaymentRecord
// -----------------------------------------------------------------------------

// payzenProviderData contient les champs spécifiques PayZen à
// sérialiser dans PaymentRecord.ProviderDataJSON.
type payzenProviderData struct {
	FormAction         string `json:"formAction,omitempty"`
	ReturnURL          string `json:"returnUrl,omitempty"`
	NotificationURL    string `json:"notificationUrl,omitempty"`
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`

	// Motif bancaire du refus. Dans le blob provider et non dans une
	// colonne : un code d'acquéreur appartient au protocole, et le
	// stocker ici évite une migration de schéma sur les deux backends.
	DeclineCode    string `json:"declineCode,omitempty"`
	DeclineMessage string `json:"declineMessage,omitempty"`

	// Card est la carte présentée, tant que l'issue du paiement ne
	// permet pas encore de l'enrôler. Elle doit survivre au
	// redémarrage : entre la création et le geste du porteur, il peut
	// s'écouler le temps qu'on veut, et la perdre ferait échouer
	// l'enrôlement d'un paiement pourtant accepté.
	//
	// Le PAN y est en clair, comme dans PaymentMethod.PANFull —
	// Paysim n'applique aucune protection PCI-DSS et ne doit jamais
	// voir de vraie carte.
	Card *Card `json:"card,omitempty"`
}

// payzenToRecord sérialise Transaction en PaymentRecord générique.
func payzenToRecord(tx *Transaction) (*store.PaymentRecord, error) {
	if tx == nil {
		return nil, errors.New("payzenToRecord(nil)")
	}
	if tx.Payment == nil {
		return nil, errors.New("payzenToRecord: Transaction.Payment nil")
	}
	custJSON, err := json.Marshal(tx.Customer)
	if err != nil {
		return nil, fmt.Errorf("marshal customer: %w", err)
	}
	metaJSON, err := json.Marshal(tx.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	provJSON, err := json.Marshal(payzenProviderData{
		FormAction:         tx.FormAction,
		ReturnURL:          tx.ReturnURL,
		NotificationURL:    tx.NotificationURL,
		PaymentMethodToken: tx.PaymentMethodToken,
		DeclineCode:        tx.DeclineCode,
		DeclineMessage:     tx.DeclineMessage,
		Card:               tx.Card,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal provider_data: %w", err)
	}
	return &store.PaymentRecord{
		UUID:             tx.UUID,
		Provider:         providerName,
		ProviderRef:      tx.FormToken,
		OrderID:          tx.OrderID,
		Amount:           tx.Amount,
		Currency:         tx.Currency,
		State:            tx.Payment.State(),
		Refunded:         tx.Payment.Refunded(),
		CustomerJSON:     string(custJSON),
		MetadataJSON:     string(metaJSON),
		ProviderDataJSON: string(provJSON),
		Events:           tx.Payment.Events(),
		CreatedAt:        tx.CreatedAt,
		UpdatedAt:        tx.UpdatedAt,
	}, nil
}

// -----------------------------------------------------------------------------
// Converters Subscription (payzen ⇄ SubscriptionRecord)
// -----------------------------------------------------------------------------

// payzenSubToRecord sérialise une Subscription PayZen en record générique.
// Les champs métier (EffectDate, Rrule, PaymentMethodToken) sont dans les
// colonnes typées du record ; Metadata dans MetadataJSON.
func payzenSubToRecord(sub *Subscription) (*store.SubscriptionRecord, error) {
	if sub == nil {
		return nil, errors.New("payzenSubToRecord(nil)")
	}
	metaJSON, err := json.Marshal(sub.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal subscription metadata: %w", err)
	}
	// UpdatedAt calqué sur CreatedAt tant qu'aucun update n'a lieu — le
	// modèle Subscription actuel ne trace pas de dernière modification.
	return &store.SubscriptionRecord{
		ID:                 sub.ID,
		Provider:           providerName,
		OrderID:            sub.OrderID,
		Amount:             sub.Amount,
		Currency:           sub.Currency,
		Cancelled:          sub.Cancelled,
		PaymentMethodToken: sub.PaymentMethodToken,
		EffectDate:         sub.EffectDate,
		Rrule:              sub.Rrule,
		MetadataJSON:       string(metaJSON),
		ProviderDataJSON:   "{}",
		CreatedAt:          sub.CreatedAt,
		UpdatedAt:          sub.CreatedAt,
	}, nil
}

// recordToPayzenSub désérialise. Metadata est vide si pas de JSON.
func recordToPayzenSub(rec *store.SubscriptionRecord) *Subscription {
	var metadata map[string]string
	if rec.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(rec.MetadataJSON), &metadata)
	}
	return &Subscription{
		ID:                 rec.ID,
		OrderID:            rec.OrderID,
		Amount:             rec.Amount,
		Currency:           rec.Currency,
		PaymentMethodToken: rec.PaymentMethodToken,
		EffectDate:         rec.EffectDate,
		Rrule:              rec.Rrule,
		Metadata:           metadata,
		Cancelled:          rec.Cancelled,
		CreatedAt:          rec.CreatedAt,
	}
}

// -----------------------------------------------------------------------------
// Converters PaymentMethod (payzen ⇄ PaymentMethodRecord)
// -----------------------------------------------------------------------------

// payzenMethodToRecord sérialise. Aucun champ PayZen-only pour l'instant —
// les informations 3DS d'enrôlement peuvent enrichir ProviderDataJSON quand
// on modélisera l'authentification initiale.
func payzenMethodToRecord(m *PaymentMethod) *store.PaymentMethodRecord {
	// Customer n'est fait que de chaînes : json.Marshal ne peut pas
	// échouer dessus. On ignore l'erreur plutôt que de teinter la
	// signature d'un cas qui ne se produit pas.
	custJSON, _ := json.Marshal(m.Customer)
	return &store.PaymentMethodRecord{
		Token:            m.Token,
		Provider:         providerName,
		PANFull:          m.PANFull,
		PANMasked:        m.PANMasked,
		Brand:            m.Brand,
		HolderName:       m.HolderName,
		Country:          m.Country,
		ProductCategory:  m.ProductCategory,
		IssuerName:       m.IssuerName,
		ExpiryMonth:      m.ExpiryMonth,
		ExpiryYear:       m.ExpiryYear,
		Revoked:          m.Revoked,
		CustomerJSON:     string(custJSON),
		MetadataJSON:     "{}",
		ProviderDataJSON: "{}",
		CreatedAt:        m.CreatedAt,
	}
}

// recordToPayzenMethod désérialise.
func recordToPayzenMethod(rec *store.PaymentMethodRecord) *PaymentMethod {
	// Les alias enrôlés avant l'ajout de la colonne n'ont pas de client :
	// on laisse la struct à zéro et le rejeu retombe sur celui de la
	// requête. Un JSON corrompu produit le même résultat — dégrader vaut
	// mieux qu'échouer une lecture pour un champ d'affichage.
	var customer Customer
	if rec.CustomerJSON != "" {
		_ = json.Unmarshal([]byte(rec.CustomerJSON), &customer)
	}
	return &PaymentMethod{
		Token:           rec.Token,
		PANFull:         rec.PANFull,
		PANMasked:       rec.PANMasked,
		Brand:           rec.Brand,
		HolderName:      rec.HolderName,
		Country:         rec.Country,
		ProductCategory: rec.ProductCategory,
		IssuerName:      rec.IssuerName,
		ExpiryMonth:     rec.ExpiryMonth,
		ExpiryYear:      rec.ExpiryYear,
		CreatedAt:       rec.CreatedAt,
		Revoked:         rec.Revoked,
		Customer:        customer,
	}
}

// recordToPayzen désérialise un PaymentRecord vers Transaction PayZen.
// Le domain.Payment est reconstruit via domain.Load (pas de rejeu des
// transitions — l'état persisté est présumé cohérent).
func recordToPayzen(rec *store.PaymentRecord) (*Transaction, error) {
	if rec == nil {
		return nil, nil
	}
	var customer Customer
	if err := json.Unmarshal([]byte(rec.CustomerJSON), &customer); err != nil {
		return nil, fmt.Errorf("unmarshal customer: %w", err)
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(rec.MetadataJSON), &metadata); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	var provData payzenProviderData
	if err := json.Unmarshal([]byte(rec.ProviderDataJSON), &provData); err != nil {
		return nil, fmt.Errorf("unmarshal provider_data: %w", err)
	}

	// Les dates du domain.Payment ne sont pas reprises depuis
	// PaymentRecord (qui porte celles de la Transaction enveloppe).
	// Approximation : on utilise les timestamps de la transaction —
	// équivalents en pratique pour un simulateur, et le premier event
	// (created) reste la source de vérité pour la vraie date de
	// création côté domaine.
	pay := domain.Load(rec.UUID, rec.Amount, rec.Currency, rec.State,
		rec.Refunded, rec.Events, rec.CreatedAt, rec.UpdatedAt)
	return &Transaction{
		FormToken:          rec.ProviderRef,
		UUID:               rec.UUID,
		OrderID:            rec.OrderID,
		Amount:             rec.Amount,
		Currency:           rec.Currency,
		FormAction:         provData.FormAction,
		Customer:           customer,
		Metadata:           metadata,
		Payment:            pay,
		ReturnURL:          provData.ReturnURL,
		NotificationURL:    provData.NotificationURL,
		PaymentMethodToken: provData.PaymentMethodToken,
		DeclineCode:        provData.DeclineCode,
		DeclineMessage:     provData.DeclineMessage,
		Card:               provData.Card,
		CreatedAt:          rec.CreatedAt,
		UpdatedAt:          rec.UpdatedAt,
	}, nil
}
