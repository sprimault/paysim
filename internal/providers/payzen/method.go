// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
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

// Card est l'input pour enregistrer un moyen de paiement, tel que
// fourni par les scénarios YAML ou les tests d'intégration. Reprend
// la structure minimale que le SmartForm envoie au PSP en usage réel.
// Le PAN entre en clair — aucun rejet, aucune validation Luhn,
// aveugle sur le contenu.
type Card struct {
	// PAN est le numéro complet, en clair. Aucune validation Luhn : le
	// simulateur est aveugle au contenu, hormis les quatre PAN de test
	// réservés qui déclenchent un refus.
	PAN string `json:"pan"`

	// ExpiryMonth (1-12) et ExpiryYear (4 chiffres). Une date passée
	// fait refuser tout débit, ce qui en fait un levier de test.
	ExpiryMonth int `json:"expiryMonth"`
	ExpiryYear  int `json:"expiryYear"`

	// Brand est la marque. Optionnelle : déduite du BIN si absente.
	Brand string `json:"brand,omitempty"`

	// HolderName est le nom du porteur ("DUPONT JEAN"). Optionnel :
	// un wallet n'en transmet pas. Conservé tel quel, sans
	// normalisation de casse — le marchand le relit à l'identique.
	HolderName string `json:"holderName,omitempty"`

	// Country, ProductCategory et IssuerName décrivent la carte telle
	// que l'émetteur la caractérise. Optionnels, mais ce sont eux qui
	// rendent testables la carte étrangère, la carte de débit et le
	// routage par banque — figés, ils interdisaient ces scénarios.
	// Défauts appliqués au rendu : FR, CREDIT, PAYSIM.
	Country         string `json:"country,omitempty"`         // ISO 3166-1 alpha-2
	ProductCategory string `json:"productCategory,omitempty"` // CREDIT, DEBIT, PREPAID
	IssuerName      string `json:"issuerName,omitempty"`
}

// NewPaymentMethod construit un PaymentMethod à partir d'une Card et
// d'un token pré-généré. Le brand est déduit du BIN si l'input n'en
// fournit pas — comportement identique à ce que fait PayZen en réel.
// La date CreatedAt vient de now (typiquement Clock.Now() de l'appelant).
func NewPaymentMethod(token string, card Card, now time.Time) *PaymentMethod {
	brand := card.Brand
	if brand == "" {
		brand = BrandFromBIN(card.PAN)
	}
	return &PaymentMethod{
		Token:       token,
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
	}
}

// Clock permet d'injecter une source de temps déterministe dans les
// tests. La production utilise SystemClock qui renvoie time.Now().UTC().
// Interface volontairement minimale — les besoins hors « heure
// courante » (durée, delai) restent gérés par time.After / time.Sleep.
type Clock interface {
	Now() time.Time
}

// SystemClock est l'implémentation production de Clock. Instance
// exportée pour être injectée sans construction (le zéro-valeur suffit).
type SystemClock struct{}

// Now retourne l'heure actuelle en UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

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
