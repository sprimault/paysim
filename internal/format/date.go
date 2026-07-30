// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package format

import (
	"fmt"
	"time"
)

// Convention du projet : tout time.Time circule en UTC à l'intérieur du
// processus (invariants du domaine, cohérence des logs). La conversion
// vers un fuseau local ou vers un format lisible ne se fait qu'ici, au
// moment de l'affichage.

// FormatShort rend un horodatage sous la forme "02/01/2006 15:04" en
// UTC. Format court pour les logs et les affichages compacts (tableau,
// liste). Utilise le layout Go de référence.
func FormatShort(t time.Time) string {
	return t.UTC().Format("02/01/2006 15:04")
}

// HumanDuration rend une durée en français compact : "45ms", "3s", "2min 15s",
// "1h 23min", "2j 4h". Sert à afficher les timeouts, TTL, intervalles de
// rejeu de webhook — tout ce qui a une dimension temporelle sans être
// une date. Le format s'adapte à la magnitude ; les unités à zéro sont
// omises pour rester compact.
//
// Une durée négative est traitée en valeur absolue préfixée d'un signe
// moins — utile pour signaler un écart d'horloge sans faire échouer
// silencieusement.
func HumanDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	neg := d < 0
	if neg {
		d = -d
	}
	var out string
	switch {
	case d < time.Second:
		// Sous la seconde, on n'affiche que les millisecondes — la
		// précision sub-ms est du bruit dans une UI ou un log humain.
		out = fmt.Sprintf("%dms", d/time.Millisecond)
	case d < time.Minute:
		s := int(d / time.Second)
		out = fmt.Sprintf("%ds", s)
	case d < time.Hour:
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		if s == 0 {
			out = fmt.Sprintf("%dmin", m)
		} else {
			out = fmt.Sprintf("%dmin %ds", m, s)
		}
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		if m == 0 {
			out = fmt.Sprintf("%dh", h)
		} else {
			out = fmt.Sprintf("%dh %dmin", h, m)
		}
	default:
		j := int(d / (24 * time.Hour))
		h := int((d % (24 * time.Hour)) / time.Hour)
		if h == 0 {
			out = fmt.Sprintf("%dj", j)
		} else {
			out = fmt.Sprintf("%dj %dh", j, h)
		}
	}
	if neg {
		return "-" + out
	}
	return out
}

// FormatRelative rend un délai humain entre t et ref, en français :
// "à l'instant", "il y a N minutes/heures/jours", ou "dans N …" quand
// t est postérieur à ref.
//
// Pas de calcul de mois ou d'années — la chronologie de Paysim ne
// s'étend jamais aussi loin (rétention en tampon circulaire), un délai
// exprimé en centaines de jours serait probablement le signe d'un bug
// ailleurs qu'on préfère laisser visible.
func FormatRelative(t, ref time.Time) string {
	d := ref.Sub(t)
	future := d < 0
	if future {
		d = -d
	}
	if d < time.Minute {
		return "à l'instant"
	}

	var qty int
	var unit string
	switch {
	case d < time.Hour:
		qty, unit = int(d.Minutes()), "minute"
	case d < 24*time.Hour:
		qty, unit = int(d.Hours()), "heure"
	default:
		qty, unit = int(d.Hours() / 24), "jour"
	}

	plur := ""
	if qty > 1 {
		plur = "s"
	}
	if future {
		return fmt.Sprintf("dans %d %s%s", qty, unit, plur)
	}
	return fmt.Sprintf("il y a %d %s%s", qty, unit, plur)
}
