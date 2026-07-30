// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package format

import (
	"strconv"
	"strings"
)

// nbsp est l'espace insécable (U+00A0), séparateur de milliers en
// convention française. Cohérent avec ce que money.Parse accepte en
// entrée — ce qu'on écrit ici, on peut le relire là-bas sans conversion
// intermédiaire.
const nbsp = " "

// Int formate un entier signé avec séparateurs de milliers en espace
// insécable. Convention française : "1234567" devient "1 234 567"
// (chaque séparateur étant U+00A0). Le signe éventuel reste collé au
// premier groupe.
//
// Les entiers de trois chiffres ou moins passent tels quels, sans
// séparateur — c'est ce qui rend le résultat propre pour un cumul qui
// démarre à zéro et grandit.
func Int(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	raw := strconv.FormatInt(n, 10)
	if len(raw) <= 3 {
		if neg {
			return "-" + raw
		}
		return raw
	}

	// On insère les séparateurs en partant de la gauche : la taille du
	// premier groupe est le reste modulo 3 (ou 3 si divisible), puis
	// tous les groupes suivants font exactement 3 chiffres.
	first := len(raw) % 3
	if first == 0 {
		first = 3
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(raw[:first])
	for i := first; i < len(raw); i += 3 {
		b.WriteString(nbsp)
		b.WriteString(raw[i : i+3])
	}
	return b.String()
}
