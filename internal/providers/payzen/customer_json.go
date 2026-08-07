// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Sérialisation des blocs du contexte client, alignée sur PayZen.
//
// PayZen expose ces objets en entier, champs compris, valorisés à null
// quand ils sont vides :
//
//	"billingDetails": { "address": null, "firstName": null, … }
//
// Le comportement Go par défaut donne deux écarts. Avec `omitempty`, la
// clé disparaît — écart structurel : Object.keys(), l'itération, un `in`
// et un typage non optionnel divergent tous, et c'est ce qui produit un
// « ça marchait en test ». Sans `omitempty`, la clé sort à `""` — écart
// de valeur, plus discret mais réel : `firstName ?? "N/A"` rend "N/A"
// chez PayZen et "" ici, puisque la chaîne vide n'est pas nullish.
//
// D'où ce marshaleur. Les champs restent des string dans le modèle —
// aucun pointeur, aucun déréférencement, aucun risque de nil — et la
// conversion se fait au seul moment où elle compte, la sortie. À
// l'entrée rien à faire : encoding/json mappe déjà null sur la valeur
// zéro.

// marshalNullingEmpty sérialise v en remplaçant les chaînes vides par
// null, en respectant les noms de champs des tags json.
//
// Réflexion plutôt qu'une struct miroir par type : trois blocs et une
// cinquantaine de champs, dont la liste bouge au gré du protocole. Une
// duplication manuelle dériverait au premier ajout — c'est précisément
// le genre de recopie qui a fait disparaître customer.reference.
func marshalNullingEmpty(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	rt := rv.Type()

	// Un map ordonne ses clés alphabétiquement à la sérialisation, ce
	// qui suffit : aucun consommateur ne dépend de l'ordre des champs
	// d'un objet JSON.
	out := make(map[string]any, rt.NumField())
	for i := range rt.NumField() {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue // non exporté
		}
		nom := nomJSON(f)
		if nom == "-" {
			continue
		}
		val := rv.Field(i)
		if val.Kind() == reflect.String && val.String() == "" {
			out[nom] = nil
			continue
		}
		out[nom] = val.Interface()
	}
	return json.Marshal(out)
}

// nomJSON retourne le nom sérialisé d'un champ : celui du tag json s'il
// existe, le nom Go sinon. Les options du tag (omitempty…) sont ignorées
// — ce marshaleur émet tout, c'est son objet.
func nomJSON(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	nom, _, _ := strings.Cut(tag, ",")
	if nom == "" {
		return f.Name
	}
	return nom
}

// MarshalJSON émet l'adresse de facturation avec tous ses champs, les
// vides à null. Le type alias coupe la récursion : sans lui, la méthode
// s'appellerait elle-même à travers la réflexion.
func (b BillingDetails) MarshalJSON() ([]byte, error) {
	type alias BillingDetails
	return marshalNullingEmpty(alias(b))
}

// MarshalJSON émet l'adresse de livraison avec tous ses champs.
func (s ShippingDetails) MarshalJSON() ([]byte, error) {
	type alias ShippingDetails
	return marshalNullingEmpty(alias(s))
}

// MarshalJSON émet le contexte navigateur avec tous ses champs.
func (e ExtraDetails) MarshalJSON() ([]byte, error) {
	type alias ExtraDetails
	return marshalNullingEmpty(alias(e))
}

// MarshalJSON émet le bloc client entier : email et reference à null
// quand ils sont vides, et les trois sous-blocs toujours présents —
// leurs propres MarshalJSON prennent alors le relais.
func (c Customer) MarshalJSON() ([]byte, error) {
	type alias Customer
	return marshalNullingEmpty(alias(c))
}
