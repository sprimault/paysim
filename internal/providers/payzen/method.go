// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PaymentMethod est un moyen de paiement enregistré chez Paysim. Il
// est adressable par Token (le `paymentMethodToken` que PayZen
// retourne dans le kr-answer après un formAction REGISTER_PAY ou
// ASK_REGISTER_PAY). Un marchand peut ensuite créer un paiement
// récurrent sans formulaire en passant ce Token à
// POST /V4/Charge/CreatePayment.
//
// AVERTISSEMENT : Paysim est un simulateur, aucune protection
// PCI-DSS n'est appliquée. Le PAN est stocké en clair dans PANFull.
// N'utilisez JAMAIS ce store avec de vraies cartes en usage.
type PaymentMethod struct {
	// Token est l'alias opaque rendu au marchand — le
	// paymentMethodToken à repasser pour débiter sans formulaire.
	Token string

	// Provider est la marque Lyra sous laquelle la carte a été
	// enrôlée. Nommé ainsi et non Brand, déjà pris ici par la marque
	// de la carte : dans cette struct, Brand veut dire VISA, pas
	// Systempay.
	Provider string

	// PANFull est le numéro complet, stocké en clair : aucune
	// protection PCI-DSS, jamais de vraie carte ici. PANMasked en est
	// la forme tronquée, la seule que l'API expose.
	PANFull   string
	PANMasked string

	// Brand est la marque, déduite du BIN si l'enrôlement ne la donne
	// pas. HolderName est le nom du porteur, vide si non fourni.
	Brand      string
	HolderName string

	// ExpiryMonth (1-12) et ExpiryYear (4 chiffres). La carte reste
	// valide jusqu'au dernier jour de son mois d'expiration.
	ExpiryMonth int
	ExpiryYear  int

	// CreatedAt en UTC. Pas d'UpdatedAt : un moyen enregistré ne se
	// modifie pas, il se révoque et un nouveau prend le relais.
	CreatedAt time.Time

	// Revoked marque une révocation explicite par le marchand. Un
	// moyen révoqué reste stocké — c'est ce qui permet de rejouer un
	// impayé dessus — mais tout débit le refuse.
	Revoked bool

	// Caractérisation par l'émetteur. Vides quand l'enrôlement ne les
	// a pas fournis ; le rendu applique alors ses défauts.
	Country         string
	ProductCategory string
	IssuerName      string

	// Customer est le client tel qu'il était à l'enrôlement. C'est lui
	// qui fait autorité lors d'un rejeu : PayZen ignore les
	// customer.reference, customer.email et customer.billingDetails
	// transmis dans la requête et restitue ceux de l'alias.
	//
	// L'alias appartient au client, pas à la commande — de là découle
	// tout le reste. Un marchand qui se trompe de référence au moment
	// d'un prélèvement récurrent ne s'en apercevra pas chez PayZen, et
	// ne doit pas s'en apercevoir ici non plus : un simulateur plus
	// logique que le vrai masque le défaut au lieu de le révéler.
	//
	// Vide sur les alias enrôlés avant cette version — le rejeu retombe
	// alors sur le client de la requête, faute de mieux.
	Customer Customer
}

// IsExpired retourne true si le mois/année d'expiration sont
// strictement antérieurs au mois/année de now. Une carte qui expire
// « ce mois-ci » est encore valide jusqu'à la fin du mois — c'est
// la convention bancaire française.
func (m *PaymentMethod) IsExpired(now time.Time) bool {
	return isExpired(m.ExpiryMonth, m.ExpiryYear, now)
}

// isExpired porte la règle sur les champs bruts, pour que MethodUsability
// puisse l'appliquer à un record générique sans reconstruire un
// PaymentMethod.
func isExpired(expiryMonth, expiryYear int, now time.Time) bool {
	year, month, _ := now.Date()
	if expiryYear < year {
		return true
	}
	if expiryYear == year && expiryMonth < int(month) {
		return true
	}
	return false
}

// NewPaymentMethod construit un PaymentMethod à partir d'une Card, d'un
// token pré-généré et du client de la transaction qui l'enrôle.
//
// Le client est capturé ici et nulle part ailleurs : c'est le seul
// moment où l'alias apprend à qui il appartient.
//
// Le brand est déduit du BIN si l'input n'en fournit pas — comportement
// identique à ce que fait PayZen en réel. La date CreatedAt vient de now
// (typiquement Clock.Now() de l'appelant).
// ErrInvalidCard rejette une carte qu'on ne peut pas enrôler.
var ErrInvalidCard = errors.New("carte invalide")

// Validate contrôle ce sans quoi un alias ne veut rien dire : un numéro,
// et une date d'expiration lisible.
//
// Rien de plus. Pas de Luhn, pas de longueur : le simulateur reste
// aveugle au contenu du PAN, hormis les numéros de test réservés. On
// vérifie ce qui a un sens ici, pas ce qu'une banque vérifierait.
//
// L'absence de ce contrôle produisait un alias en 0/0, aussitôt réputé
// expiré. Tout ce qui en dérivait annonçait alors une expiration qui
// n'avait pas eu lieu : le moyen était rendu inexploitable, le paiement
// refusé pour « moyen de paiement expire », et le marchand cherchait une
// date périmée qu'il n'avait jamais envoyée. Un enrôlement qu'on ne peut
// pas honorer doit échouer bruyamment, pas produire un alias mort-né.
func (c Card) Validate() error {
	if c.PAN == "" {
		return fmt.Errorf("%w: pan absent", ErrInvalidCard)
	}
	if c.ExpiryMonth < 1 || c.ExpiryMonth > 12 {
		return fmt.Errorf("%w: expiryMonth = %d, attendu 1-12", ErrInvalidCard, c.ExpiryMonth)
	}
	// Quatre chiffres exigés : un « 28 » pour 2028 passerait pour une
	// date de l'an 28, donc expirée, et on retomberait exactement sur le
	// défaut qu'on corrige.
	if c.ExpiryYear < 1000 {
		return fmt.Errorf("%w: expiryYear = %d, attendu sur 4 chiffres", ErrInvalidCard, c.ExpiryYear)
	}
	return nil
}

// NewPaymentMethod construit l'alias enrôlé à partir de la carte
// présentée. La marque de la carte se déduit du BIN quand l'enrôlement
// ne la donne pas ; celle de l'intégration, elle, ne se devine pas — un
// alias enrôlé en Systempay ne se distingue en rien d'un alias PayZen,
// et le seul endroit qui la connaît est la transaction. D'où le
// paramètre : l'oublier ne compile pas.
func NewPaymentMethod(token, provider string, card Card, customer Customer, now time.Time) *PaymentMethod {
	brand := card.Brand
	if brand == "" {
		brand = BrandFromBIN(card.PAN)
	}
	return &PaymentMethod{
		Token:       token,
		Provider:    marqueOuDefaut(provider),
		PANFull:     card.PAN,
		PANMasked:   maskPAN(card.PAN),
		Brand:       brand,
		HolderName:  card.HolderName,
		ExpiryMonth: card.ExpiryMonth,
		ExpiryYear:  card.ExpiryYear,
		CreatedAt:   now,

		Country:         card.Country,
		ProductCategory: card.ProductCategory,
		IssuerName:      card.IssuerName,

		Customer: customer,
	}
}

// maskPAN retourne le PAN masqué au format PayZen : 6 premiers
// chiffres visibles + X pour le milieu + 4 derniers visibles. C'est
// la représentation qu'on trouve dans les kr-answer et les logs
// marchand — reproduite telle quelle (invariant 3).
//
// Pour un PAN trop court pour supporter le format 6+4, on retombe
// sur les 4 derniers visibles seulement. Un PAN vide retourne "".
func maskPAN(pan string) string {
	n := len(pan)
	switch {
	case n == 0:
		return ""
	case n <= 4:
		return strings.Repeat("X", n)
	case n < 10:
		return strings.Repeat("X", n-4) + pan[n-4:]
	default:
		return pan[:6] + strings.Repeat("X", n-10) + pan[n-4:]
	}
}

// BrandFromBIN déduit la marque d'une carte à partir des premiers
// chiffres du PAN (BIN — Bank Identification Number). Couvre les
// marques les plus courantes, sans co-marquage CB français (un PAN
// Visa/Mastercard émis en France est aussi CB, mais la co-marque
// n'est pas dérivable du BIN seul — le simulateur reste sur la
// marque internationale visible).
//
// Retourne "" si le BIN ne matche aucune règle connue.
func BrandFromBIN(pan string) string {
	if len(pan) < 2 {
		return ""
	}
	if pan[0] == '4' {
		return "VISA"
	}
	first2, err := strconv.Atoi(pan[:2])
	if err == nil {
		if first2 >= 51 && first2 <= 55 {
			return "MASTERCARD"
		}
		if first2 == 34 || first2 == 37 {
			return "AMEX"
		}
	}
	if len(pan) >= 4 {
		first4, err := strconv.Atoi(pan[:4])
		if err == nil && first4 >= 2221 && first4 <= 2720 {
			return "MASTERCARD"
		}
	}
	return ""
}

// IsLuhnValid retourne true si le PAN passe la vérification Luhn
// (algorithme de checksum standard des numéros de carte). Utilisé
// uniquement en informatif — Paysim n'effectue AUCUN rejet basé sur
// Luhn, c'est un simulateur. Un logger peut
// néanmoins avertir si un scénario utilise un PAN qui échoue Luhn,
// pour aider un dev qui saisit une valeur bidon par erreur.
func IsLuhnValid(pan string) bool {
	if pan == "" {
		return false
	}
	sum := 0
	alt := false
	for i := len(pan) - 1; i >= 0; i-- {
		c := pan[i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}
