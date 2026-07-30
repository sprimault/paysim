// Copyright (c) 2026 Paysim by Stéphane Primault — Tous droits réservés.
// Author: Stéphane Primault <sprimault@users.noreply.github.com>

// Package format regroupe le formatage partagé entre les paquets applicatifs.
// money.go est le premier bloc posé : tous les montants Paysim sont des entiers
// en centimes, jamais des flottants, et la conversion vers l'affichage passe
// exclusivement par ici.
package format

import (
	"errors"
	"strings"
)

// Amount est un montant en centimes de la devise en cours. Les entrées et
// sorties textuelles sont en unité principale (euros, dollars…), la
// représentation interne est en centimes pour éliminer toute arithmétique
// flottante.
type Amount int64

var (
	ErrEmpty         = errors.New("montant vide")
	ErrInvalidFormat = errors.New("montant mal formé")
	ErrOverflow      = errors.New("montant hors limites")
)

// Parse lit un montant en unité principale et retourne sa valeur en centimes.
// Les formats acceptés sont "12", "12,34", "12.34", avec espace ou espace
// insécable comme séparateur de milliers, et un signe optionnel en tête.
// Une partie décimale de zéro à deux chiffres est acceptée ; au-delà, refus.
func Parse(s string) (Amount, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrEmpty
	}

	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, ErrInvalidFormat
	}

	intPart, fracPart, hasFrac := splitDecimal(s)
	intPart, ok := stripThousandSeps(intPart)
	if !ok || intPart == "" || !allDigits(intPart) {
		return 0, ErrInvalidFormat
	}
	if hasFrac && (len(fracPart) == 0 || len(fracPart) > 2 || !allDigits(fracPart)) {
		return 0, ErrInvalidFormat
	}

	// Normalise la partie décimale à deux chiffres.
	switch len(fracPart) {
	case 0:
		fracPart = "00"
	case 1:
		fracPart = fracPart + "0"
	}

	cents, err := atoiPositive(intPart + fracPart)
	if err != nil {
		return 0, err
	}
	if neg {
		cents = -cents
	}
	return Amount(cents), nil
}

// String retourne la représentation en français : "12,34". Pas de séparateur de
// milliers, pas de symbole monétaire — ces choix relèvent de l'appelant.
func (a Amount) String() string {
	neg := a < 0
	n := int64(a)
	if neg {
		n = -n
	}
	cents := n % 100
	units := n / 100

	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(itoa(units))
	b.WriteByte(',')
	if cents < 10 {
		b.WriteByte('0')
	}
	b.WriteString(itoa(cents))
	return b.String()
}

// splitDecimal isole la partie entière et la partie décimale d'un montant
// textuel. Le séparateur décimal reconnu est ',' ou '.' ; la présence de
// deux occurrences est un format ambigu et le résultat est signalé sans
// partie décimale (hasFrac faux), ce qui provoquera un rejet en aval via
// la vérification allDigits.
func splitDecimal(s string) (intPart, fracPart string, hasFrac bool) {
	// Le séparateur décimal est le premier ',' ou '.' rencontré ; un second
	// serait ambigu — on refuse plutôt que de deviner.
	idx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ',' || s[i] == '.' {
			if idx != -1 {
				// Deux séparateurs décimaux : format ambigu, on refuse.
				return s, "", false
			}
			idx = i
		}
	}
	if idx == -1 {
		return s, "", false
	}
	return s[:idx], s[idx+1:], true
}

// stripThousandSeps retire les séparateurs de milliers et vérifie qu'ils sont
// bien placés : premier groupe de 1 à 3 chiffres, groupes suivants d'exactement
// 3 chiffres. Le booléen est faux si la structure n'est pas respectée — sinon
// "12 34" serait silencieusement lu comme 1234.
// Espaces reconnus : espace ordinaire et espace insécable (U+00A0).
func stripThousandSeps(s string) (string, bool) {
	if !strings.ContainsAny(s, " ") && !strings.Contains(s, " ") {
		return s, true
	}
	s = strings.ReplaceAll(s, " ", " ")
	parts := strings.Split(s, " ")
	if n := len(parts[0]); n == 0 || n > 3 {
		return "", false
	}
	for _, p := range parts[1:] {
		if len(p) != 3 {
			return "", false
		}
	}
	return strings.Join(parts, ""), true
}

// allDigits retourne vrai si s est composé exclusivement de chiffres ASCII.
// Une chaîne vide retourne vrai — l'appelant filtre le cas vide séparément
// quand il est significatif.
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// atoiPositive convertit une chaîne de chiffres en int64 en détectant le
// dépassement. Utiliser strconv.ParseInt suffirait, mais son message d'erreur
// laisse fuiter la chaîne source dans les logs ; on garde la main.
func atoiPositive(s string) (int64, error) {
	const max = int64(9_223_372_036_854_775_807)
	var n int64
	for i := 0; i < len(s); i++ {
		d := int64(s[i] - '0')
		if n > (max-d)/10 {
			return 0, ErrOverflow
		}
		n = n*10 + d
	}
	return n, nil
}

// itoa convertit un entier positif en chaîne décimale sans passer par
// strconv.Itoa. Un tampon de 20 octets couvre tout int64 positif (au plus
// 19 chiffres) ; les chiffres sont écrits du poids faible au poids fort en
// remontant dans le tampon, puis on tranche à partir de la position atteinte.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
