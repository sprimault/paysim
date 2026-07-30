// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package format

import "testing"

func TestTruncate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"", 5, ""},
		{"abc", 5, "abc"},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcde…"},
		{"hello world", 5, "hello…"},
		{"héllo", 3, "hél…"}, // frontière UTF-8 respectée
		{"éàü", 2, "éà…"},
		{"abc", 0, "…"},
		{"abc", -1, "…"},
		{"", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			if got := Truncate(c.in, c.max); got != c.want {
				t.Errorf("Truncate(%q, %d) = %q, veut %q", c.in, c.max, got, c.want)
			}
		})
	}
}

func TestMask(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		in             string
		prefix, suffix int
		want           string
	}{
		{"pan 16 chiffres", "4111111111111111", 4, 4, "4111********1111"},
		{"signature longue", "abcdef1234567890abcdef", 4, 4, "abcd**************cdef"},
		{"secret trop court", "abc", 1, 1, "***"},
		{"marge insuffisante", "abcdef", 2, 2, "***"}, // 2+2+3 > 6 → opaque
		{"marge tout juste", "abcdefg", 2, 2, "ab***fg"},
		{"chaine vide", "", 4, 4, "***"},
		{"prefix suffix negatifs", "4111111111111111", -1, -1, "****************"},
		{"prefix seul", "abcdefgh", 4, 0, "abcd****"},
		{"suffix seul", "abcdefgh", 0, 4, "****efgh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Mask(c.in, c.prefix, c.suffix); got != c.want {
				t.Errorf("Mask(%q, %d, %d) = %q, veut %q", c.in, c.prefix, c.suffix, got, c.want)
			}
		})
	}
}
