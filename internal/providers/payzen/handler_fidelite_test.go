// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"testing"

	"github.com/sprimault/paysim/internal/clock"
)

// Écarts entre ce que la référence annonce et ce que le binaire faisait.
// Trouvés en comparant docs/providers/payzen.md au code, puis confirmés
// par HTTP contre un vrai binaire — les tests unitaires les avaient
// laissés passer parce qu'ils vérifiaient l'acceptation, pas la
// restitution.

// TestCreateSubscriptionNatifRefuseAliasInconnu : la route native
// acceptait n'importe quel token non vide. L'abonnement se créait,
// Subscription/Get le confirmait, l'interface l'affichait — et chaque
// échéance échouait ensuite, sans que rien n'ait annoncé le problème au
// moment où il pouvait encore être corrigé. Le chemin générique
// vérifiait déjà ; les deux se rejoignent.
func TestCreateSubscriptionNatifRefuseAliasInconnu(t *testing.T) {
	t.Parallel()
	server, _ := newTestServer(t)

	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription",
		CreateSubscriptionRequest{
			Amount: 500, Currency: "EUR",
			PaymentMethodToken: "pmt-qui-nexiste-pas",
			EffectDate:         "2026-09-01T00:00:00Z",
			Rrule:              "RRULE:FREQ=MONTHLY;INTERVAL=1",
		}, "u", "p")

	if resp.Status != "ERROR" {
		t.Fatalf("Status = %q, veut ERROR — un abonnement ne se crée pas sur un alias absent", resp.Status)
	}
	var e APIError
	_ = json.Unmarshal(resp.Answer, &e)
	if e.ErrorCode != ErrCodePaymentMethodUnknown {
		t.Errorf("ErrorCode = %q, veut %q", e.ErrorCode, ErrCodePaymentMethodUnknown)
	}
}

// TestCreateSubscriptionNatifPorteLaMarqueDeLInstance : second écart du
// même endroit. La route native n'attribuait aucune marque, si bien
// qu'une instance configurée sur une autre marque enregistrait quand
// même ses abonnements sous celle par défaut.
func TestCreateSubscriptionNatifPorteLaMarqueDeLInstance(t *testing.T) {
	t.Parallel()
	server, store, _ := newTestServerFull(t, HandlerConfig{
		HMACKey: "k", RESTPassword: "p", Brand: "systempay",
	})
	poserAlias(t, store, "pmt-marque")

	resp, _ := post(t, server.URL+"/api-payment/V4/Charge/CreateSubscription",
		CreateSubscriptionRequest{
			Amount: 500, Currency: "EUR", PaymentMethodToken: "pmt-marque",
			EffectDate: "2026-09-01T00:00:00Z",
			Rrule:      "RRULE:FREQ=MONTHLY;INTERVAL=1",
		}, "u", "p")
	if resp.Status != "SUCCESS" {
		t.Fatalf("Status = %q, veut SUCCESS", resp.Status)
	}
	var a CreateSubscriptionAnswer
	_ = json.Unmarshal(resp.Answer, &a)

	sub, err := store.SubscriptionByID(a.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil {
		t.Fatal("abonnement introuvable après création")
	}
	if sub.Brand != "systempay" {
		t.Errorf("Brand = %q, veut %q — l'abonnement doit hériter de la marque de l'instance",
			sub.Brand, "systempay")
	}
}

// TestThreeDSChallengeNeSortPasDuJeuDeStatuts : threeDSStatus décrit à la
// fois l'issue et la manière. CHALLENGE est une manière — un challenge
// réussi se solde par SUCCESS. Le recopier dans Status y plaçait une
// valeur que la référence n'énumère pas.
func TestThreeDSChallengeNeSortPasDuJeuDeStatuts(t *testing.T) {
	t.Parallel()

	cas := []struct {
		entree     string
		veutStatus string
		veutType   string
	}{
		{"CHALLENGE", "SUCCESS", "CHALLENGE"},
		{"SUCCESS", "SUCCESS", "FRICTIONLESS"},
		{"FAILURE", "FAILURE", "FRICTIONLESS"},
		{"NOT_ENROLLED", "NOT_ENROLLED", "FRICTIONLESS"},
	}
	for _, c := range cas {
		t.Run(c.entree, func(t *testing.T) {
			t.Parallel()
			tx := &Transaction{
				UUID: "tx-3ds", OrderID: "o-3ds", Amount: 1000, Currency: "EUR",
			}
			ans := buildKrAnswer(clock.System{}, tx, nil, nil, BrowserReturnOpts{
				Outcome: OutcomePaid, ThreeDSStatus: c.entree,
			}, "", "TEST")

			td := ans.Transactions[0].TransactionDetails
			if td.ThreeDSResponse == nil {
				t.Fatal("bloc 3DS absent du kr-answer")
			}
			got := td.ThreeDSResponse.AuthenticationResultData
			if got.Status != c.veutStatus {
				t.Errorf("status = %q, veut %q", got.Status, c.veutStatus)
			}
			if got.AuthenticationType != c.veutType {
				t.Errorf("authenticationType = %q, veut %q", got.AuthenticationType, c.veutType)
			}
		})
	}
}
