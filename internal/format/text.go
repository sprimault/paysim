// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package format

import "strings"

// Truncate coupe une chaîne à `max` runes en préservant les frontières
// UTF-8. Si la chaîne est effectivement tronquée, l'ellipse Unicode
// (U+2026) est ajoutée en suffixe. La borne `max` s'entend hors
// ellipse : Truncate("hello", 3) rend "hel…" (3 runes visibles).
//
// max négatif est traité comme zéro — on préfère un comportement
// défini à un panic sur un cas facile à provoquer depuis de la donnée
// externe.
func Truncate(s string, max int) string {
	if max < 0 {
		max = 0
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// Mask masque le milieu d'une chaîne en gardant `prefix` runes en tête
// et `suffix` runes en queue ; le milieu est remplacé par des étoiles
// d'une longueur égale au nombre de runes masquées. Sert à loguer un
// PAN, une signature, un jeton sans compromettre le secret.
//
// Si prefix+suffix+3 dépasse la longueur de la chaîne, la totalité est
// masquée par "***" — la marge de 3 étoiles garantit qu'on n'expose
// pas la quasi-totalité d'un secret court sous couvert de « masquage ».
//
// prefix et suffix négatifs sont traités comme zéro, pour la même
// raison que Truncate.
func Mask(s string, prefix, suffix int) string {
	if prefix < 0 {
		prefix = 0
	}
	if suffix < 0 {
		suffix = 0
	}
	r := []rune(s)
	if prefix+suffix+3 > len(r) {
		return "***"
	}
	middle := len(r) - prefix - suffix
	return string(r[:prefix]) + strings.Repeat("*", middle) + string(r[len(r)-suffix:])
}
