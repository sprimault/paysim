// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"fmt"
	"time"
)

// Sérialisation des instants pour SQLite.
//
// SQLite n'a pas de type date : les instants sont stockés en texte, et
// la convention — UTC, RFC 3339 avec les nanosecondes — n'était écrite
// nulle part. Elle se déduisait de dix-huit répétitions dans cinq
// dépôts, ce qui suffit à la faire diverger : il aurait suffi qu'un
// nouveau dépôt omette le `.UTC()` pour que ses dates se comparent mal
// à celles des autres, sans que rien ne le signale.
//
// Le format compte : RFC3339Nano conserve la précision d'origine, et
// c'est ce que le tri par `updated_at` exige — deux écritures d'une
// même milliseconde doivent rester ordonnées.

// horodater rend la forme stockée d'un instant : UTC, RFC 3339 avec les
// nanosecondes. Passer par ici plutôt que par time.Format garantit que
// tous les dépôts écrivent la même chose.
func horodater(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// lireHorodatage relit ce qu'a écrit horodater. Le nom de colonne entre
// dans le message d'erreur : sur une base rafistolée à la main, savoir
// laquelle a échoué évite de relire cinq dépôts.
func lireHorodatage(colonne, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s %q: %w", colonne, s, err)
	}
	return t, nil
}
