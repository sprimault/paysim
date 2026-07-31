// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

// panPrefixByBrand donne les 4 premiers chiffres d'une carte fictive
// par brand, cohérents avec les IIN publiques réels : Visa commence
// par 4, Mastercard par 5, etc. Sert à générer des PAN masqués de
// démonstration crédibles dans kr-answer sans jamais utiliser de
// vraie carte (règle providers.md : les PAN ne se loguent pas en clair,
// et par extension une simulation ne doit pas prétendre porter une
// vraie donnée sensible).
var panPrefixByBrand = map[string]string{
	"VISA":        "4970",
	"MASTERCARD":  "5555",
	"CB":          "4970",
	"AMEX":        "3782",
	"MAESTRO":     "5018",
	"VISA_DEBIT":  "4462",
	"VISA_CREDIT": "4970",
}

// newMaskedPAN construit un PAN de démonstration au format
// "IIN + '**********' + '0000'" (16 caractères) pour un brand donné.
// Utilise un préfixe IIN reconnu pour que le brand se déduise
// correctement même dans des systèmes qui inspectent les 4 premiers
// chiffres. Le suffixe reste "0000" constant — ce n'est pas un secret,
// c'est un identifiant de fixture.
func newMaskedPAN(brand string) string {
	prefix, ok := panPrefixByBrand[brand]
	if !ok {
		prefix = panPrefixByBrand["VISA"]
	}
	return prefix + "XXXXXXXX0000"
}
