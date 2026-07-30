// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package format

import (
	"testing"
	"time"
)

func TestFormatShort(t *testing.T) {
	t.Parallel()
	// Construit en UTC : le rendu doit être identique quelle que soit
	// la locale de la machine qui exécute le test.
	ts := time.Date(2026, 3, 12, 14, 23, 45, 0, time.UTC)
	if got, want := FormatShort(ts), "12/03/2026 14:23"; got != want {
		t.Errorf("FormatShort = %q, veut %q", got, want)
	}
}

func TestFormatShortConvertToUTC(t *testing.T) {
	t.Parallel()
	// Un time.Time en fuseau non-UTC doit être ramené en UTC avant
	// affichage — sinon on aurait des rendus divergents selon l'origine
	// des données.
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("chargement fuseau: %v", err)
	}
	ts := time.Date(2026, 6, 15, 16, 0, 0, 0, loc) // 16h Paris = 14h UTC en été
	if got, want := FormatShort(ts), "15/06/2026 14:00"; got != want {
		t.Errorf("FormatShort (Paris été) = %q, veut %q", got, want)
	}
}

func TestHumanDuration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Microsecond, "0ms"},
		{45 * time.Millisecond, "45ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1s"},
		{3 * time.Second, "3s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1min"},
		{2*time.Minute + 15*time.Second, "2min 15s"},
		{59 * time.Minute, "59min"},
		{time.Hour, "1h"},
		{time.Hour + 23*time.Minute, "1h 23min"},
		{23*time.Hour + 59*time.Minute, "23h 59min"},
		{24 * time.Hour, "1j"},
		{2*24*time.Hour + 4*time.Hour, "2j 4h"},
		{-5 * time.Second, "-5s"},
		{-time.Hour, "-1h"},
	}
	for _, c := range cases {
		if got := HumanDuration(c.in); got != c.want {
			t.Errorf("HumanDuration(%v) = %q, veut %q", c.in, got, c.want)
		}
	}
}

func TestFormatRelative(t *testing.T) {
	t.Parallel()
	ref := time.Date(2026, 3, 12, 14, 23, 45, 0, time.UTC)

	cases := []struct {
		name   string
		offset time.Duration // négatif = passé, positif = futur
		want   string
	}{
		{"instant passe", -30 * time.Second, "à l'instant"},
		{"instant futur", +30 * time.Second, "à l'instant"},
		{"1 minute passee", -1 * time.Minute, "il y a 1 minute"},
		{"2 minutes passees", -2 * time.Minute, "il y a 2 minutes"},
		{"1 heure passee", -1 * time.Hour, "il y a 1 heure"},
		{"3 heures passees", -3 * time.Hour, "il y a 3 heures"},
		{"1 jour passe", -24 * time.Hour, "il y a 1 jour"},
		{"5 jours passes", -5 * 24 * time.Hour, "il y a 5 jours"},
		{"2 minutes futures", +2 * time.Minute, "dans 2 minutes"},
		{"1 jour futur", +24 * time.Hour, "dans 1 jour"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ts := ref.Add(c.offset)
			if got := FormatRelative(ts, ref); got != c.want {
				t.Errorf("FormatRelative offset=%v = %q, veut %q", c.offset, got, c.want)
			}
		})
	}
}
