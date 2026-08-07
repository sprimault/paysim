// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"testing"
)

// decode ramène un JSON à une map, pour distinguer trois états qu'une
// struct Go confond : clé absente, clé à null, clé valorisée.
func decode(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// L'écart structurel est le pire des deux : une clé absente fait diverger
// Object.keys(), l'itération, un `in` et un typage non optionnel. PayZen
// expose toujours la structure entière.
func TestCustomerVideExposeToutesLesCles(t *testing.T) {
	t.Parallel()
	m := decode(t, Customer{})

	for _, cle := range []string{"email", "reference", "billingDetails", "shippingDetails", "extraDetails"} {
		if _, present := m[cle]; !present {
			t.Errorf("clé %q absente d'un customer vide", cle)
		}
	}
	if m["email"] != nil {
		t.Errorf("email = %v, veut null", m["email"])
	}

	billing, ok := m["billingDetails"].(map[string]any)
	if !ok {
		t.Fatalf("billingDetails = %T, veut un objet", m["billingDetails"])
	}
	if len(billing) != 8 {
		t.Errorf("billingDetails porte %d champs, veut 8 — un objet vide n'est pas ce que rend PayZen", len(billing))
	}
	for cle, v := range billing {
		if v != nil {
			t.Errorf("billingDetails.%s = %v, veut null", cle, v)
		}
	}
}

// L'écart de valeur : "" n'est pas nullish, donc `x ?? "N/A"` diverge
// silencieusement entre un vrai PSP et un simulateur qui rend "".
func TestChampVideSortEnNullPasEnChaineVide(t *testing.T) {
	t.Parallel()
	m := decode(t, Customer{Email: "a@example.com"})

	if m["email"] != "a@example.com" {
		t.Errorf("email = %v, veut la valeur fournie", m["email"])
	}
	if m["reference"] != nil {
		t.Errorf("reference = %#v, veut null et non une chaîne vide", m["reference"])
	}
}

// Un champ renseigné au milieu de champs vides doit ressortir intact :
// c'est le cas courant, et l'erreur classique d'un marshaleur maison.
func TestChampRenseigneSurvitAuxNull(t *testing.T) {
	t.Parallel()
	m := decode(t, Customer{
		BillingDetails:  BillingDetails{LastName: "MARTIN"},
		ShippingDetails: ShippingDetails{ShippingMethod: "RELAY_POINT"},
		ExtraDetails:    ExtraDetails{IPAddress: "203.0.113.7"},
	})

	billing := m["billingDetails"].(map[string]any)
	if billing["lastName"] != "MARTIN" {
		t.Errorf("lastName = %v, veut MARTIN", billing["lastName"])
	}
	if billing["firstName"] != nil {
		t.Errorf("firstName = %v, veut null", billing["firstName"])
	}

	shipping := m["shippingDetails"].(map[string]any)
	if shipping["shippingMethod"] != "RELAY_POINT" {
		t.Errorf("shippingMethod = %v", shipping["shippingMethod"])
	}

	extra := m["extraDetails"].(map[string]any)
	if extra["ipAddress"] != "203.0.113.7" {
		t.Errorf("ipAddress = %v", extra["ipAddress"])
	}
}

// L'aller-retour doit être neutre : encoding/json mappe null sur la
// valeur zéro, donc rien à écrire côté décodage. Ce test verrouille cette
// gratuité — un UnmarshalJSON ajouté plus tard la casserait.
func TestAllerRetourNeutre(t *testing.T) {
	t.Parallel()
	origine := Customer{
		Email:     "a@example.com",
		Reference: "client-A",
		BillingDetails: BillingDetails{
			FirstName: "Alice", LastName: "MARTIN", Country: "FR",
		},
		ShippingDetails: ShippingDetails{City: "Lyon", ShippingSpeed: "EXPRESS"},
		ExtraDetails:    ExtraDetails{IPAddress: "203.0.113.7"},
	}

	b, err := json.Marshal(origine)
	if err != nil {
		t.Fatal(err)
	}
	var relu Customer
	if err := json.Unmarshal(b, &relu); err != nil {
		t.Fatal(err)
	}
	if relu != origine {
		t.Errorf("aller-retour non neutre\n obtenu : %+v\n veut   : %+v", relu, origine)
	}

	// Et depuis une charge entièrement à null : tout doit revenir à zéro
	// sans erreur — c'est ce que renvoie PayZen sur un paiement sans
	// contexte client.
	var depuisNull Customer
	if err := json.Unmarshal([]byte(`{
		"email": null, "reference": null,
		"billingDetails": {"firstName": null},
		"shippingDetails": {"city": null},
		"extraDetails": {"ipAddress": null}
	}`), &depuisNull); err != nil {
		t.Fatalf("décodage d'une charge à null : %v", err)
	}
	if depuisNull != (Customer{}) {
		t.Errorf("null n'a pas donné la valeur zéro : %+v", depuisNull)
	}
}
