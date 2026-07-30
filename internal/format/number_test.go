// Copyright (c) 2026 Paysim by Stéphane Primault — Tous droits réservés.
// Author: Stéphane Primault <sprimault@users.noreply.github.com>

package format

import "testing"

func TestInt(t *testing.T) {
	t.Parallel()
	// Utilise l'espace insécable (U+00A0) pour matcher exactement la
	// sortie attendue — un espace ordinaire ne collerait pas.
	const s = " "

	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{5, "5"},
		{999, "999"},
		{1000, "1" + s + "000"},
		{1234, "1" + s + "234"},
		{12345, "12" + s + "345"},
		{123456, "123" + s + "456"},
		{1234567, "1" + s + "234" + s + "567"},
		{1000000, "1" + s + "000" + s + "000"},
		{-1, "-1"},
		{-1234, "-1" + s + "234"},
		{-1234567, "-1" + s + "234" + s + "567"},
	}
	for _, c := range cases {
		got := Int(c.in)
		if got != c.want {
			t.Errorf("Int(%d) = %q, veut %q", c.in, got, c.want)
		}
	}
}
