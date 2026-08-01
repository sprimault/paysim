// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/store"
)

// SQLiteStore est l'impl PayZen du contrat Store — wrapper sur
// store.PaymentRepository (schéma générique cross-provider). Les
// champs spécifiques PayZen (Customer, Metadata, ReturnURL,
// NotificationURL, FormAction) sont sérialisés dans les blobs JSON de
// PaymentRecord ; les converters payzenToRecord / recordToPayzen
// concentrent la mécanique.
//
// Le PaymentRepository est fourni par l'appelant (typiquement
// cmd/paysim/main.go) — il peut être partagé avec d'autres providers
// et avec l'API UI cross-provider.
//
// Les abonnements PayZen sont stub côté v1 : conservés dans une map
// en mémoire au sein du SQLiteStore, non persistés. Quand un vrai
// besoin de persistance d'abonnements se manifestera, un
// SubscriptionRepository générique s'ajoutera à internal/store/.
type SQLiteStore struct {
	repo store.PaymentRepository

	// Abonnements en mémoire — stub v1.
	subMu sync.RWMutex
	subs  map[string]*Subscription
}

// providerName identifie PayZen dans la colonne payments.provider.
const providerName = "payzen"

// NewSQLiteStore construit un SQLiteStore autour du PaymentRepository
// fourni. L'appelant garde la propriété du repository (et donc la
// responsabilité de sa fermeture) — un SQLiteStore.Close() ne ferme
// pas le repo partagé.
func NewSQLiteStore(repo store.PaymentRepository) *SQLiteStore {
	return &SQLiteStore{
		repo: repo,
		subs: make(map[string]*Subscription),
	}
}

// Close est un no-op — le PaymentRepository sous-jacent est possédé
// par l'appelant (main.go) qui le ferme à shutdown.
func (s *SQLiteStore) Close() error {
	return nil
}

// -----------------------------------------------------------------------------
// Contrat Store
// -----------------------------------------------------------------------------

// Save sérialise la Transaction PayZen vers un PaymentRecord
// générique et délègue au repository.
func (s *SQLiteStore) Save(tx *Transaction) error {
	rec, err := payzenToRecord(tx)
	if err != nil {
		return err
	}
	return s.repo.Save(rec)
}

// ByToken cherche par (provider=payzen, provider_ref=FormToken).
func (s *SQLiteStore) ByToken(token string) (*Transaction, error) {
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
func (s *SQLiteStore) ByUUID(uuid string) (*Transaction, error) {
	rec, err := s.repo.ByUUID(uuid)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	// Filtre défensif : un UUID lookup pourrait matcher un autre
	// provider. Cross-provider lookup non voulu depuis un SQLiteStore
	// PayZen — on renvoie nil, cohérent avec l'API.
	if rec.Provider != providerName {
		return nil, nil
	}
	return recordToPayzen(rec)
}

// Len compte les paiements PayZen uniquement.
func (s *SQLiteStore) Len() (int, error) {
	recs, err := s.repo.ByProvider(providerName)
	if err != nil {
		return 0, err
	}
	return len(recs), nil
}

// AllTransactions retourne toutes les transactions PayZen. Ordre :
// updated_at décroissant.
func (s *SQLiteStore) AllTransactions() ([]*Transaction, error) {
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
// Abonnements — mémoire (v1 stub, à migrer si vrai besoin métier)
// -----------------------------------------------------------------------------

// SaveSubscription stocke en mémoire.
func (s *SQLiteStore) SaveSubscription(sub *Subscription) error {
	if sub == nil || sub.ID == "" {
		return nil
	}
	s.subMu.Lock()
	defer s.subMu.Unlock()
	s.subs[sub.ID] = sub
	return nil
}

// SubscriptionByID lit depuis la mémoire.
func (s *SQLiteStore) SubscriptionByID(id string) (*Subscription, error) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	return s.subs[id], nil
}

// LenSubscriptions compte les abonnements en mémoire.
func (s *SQLiteStore) LenSubscriptions() (int, error) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	return len(s.subs), nil
}

// Delete supprime une transaction PayZen. Le repo cross-provider est
// scoped par UUID unique, aucune ambiguïté possible.
func (s *SQLiteStore) Delete(uuid string) error {
	return s.repo.DeleteByUUID(uuid)
}

// DeleteAllTransactions supprime toutes les transactions PayZen —
// délègue au repo générique avec le filtre provider.
func (s *SQLiteStore) DeleteAllTransactions() (int, error) {
	return s.repo.DeleteByProvider(providerName)
}

// -----------------------------------------------------------------------------
// Converters PayZen ⇄ PaymentRecord
// -----------------------------------------------------------------------------

// payzenProviderData contient les champs spécifiques PayZen à
// sérialiser dans PaymentRecord.ProviderDataJSON.
type payzenProviderData struct {
	FormAction      string `json:"formAction,omitempty"`
	ReturnURL       string `json:"returnUrl,omitempty"`
	NotificationURL string `json:"notificationUrl,omitempty"`
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
		FormAction:      tx.FormAction,
		ReturnURL:       tx.ReturnURL,
		NotificationURL: tx.NotificationURL,
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
		FormToken:       rec.ProviderRef,
		UUID:            rec.UUID,
		OrderID:         rec.OrderID,
		Amount:          rec.Amount,
		Currency:        rec.Currency,
		FormAction:      provData.FormAction,
		Customer:        customer,
		Metadata:        metadata,
		Payment:         pay,
		ReturnURL:       provData.ReturnURL,
		NotificationURL: provData.NotificationURL,
		CreatedAt:       rec.CreatedAt,
		UpdatedAt:       rec.UpdatedAt,
	}, nil
}
