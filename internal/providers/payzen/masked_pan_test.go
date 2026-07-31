// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"strings"
	"testing"
)

func TestNewMaskedPAN(t *testing.T) {
	t.Parallel()
	cases := []struct {
		brand     string
		wantStart string
	}{
		{"VISA", "4970"},
		{"MASTERCARD", "5555"},
		{"CB", "4970"},
		{"AMEX", "3782"},
		{"MAESTRO", "5018"},
	}
	for _, c := range cases {
		t.Run(c.brand, func(t *testing.T) {
			t.Parallel()
			got := newMaskedPAN(c.brand)
			if !strings.HasPrefix(got, c.wantStart) {
				t.Errorf("newMaskedPAN(%q) = %q, veut prefix %q", c.brand, got, c.wantStart)
			}
			if len(got) != 16 {
				t.Errorf("longueur %d, veut 16", len(got))
			}
			if !strings.Contains(got, "XXXXXXXX") {
				t.Errorf("%q doit contenir un bloc XXXXXXXX de masquage", got)
			}
			if !strings.HasSuffix(got, "0000") {
				t.Errorf("%q doit se terminer par 0000", got)
			}
		})
	}
}

func TestNewMaskedPANUnknownBrand(t *testing.T) {
	t.Parallel()
	// Brand inconnue → fallback VISA (prefix 4970).
	got := newMaskedPAN("BRAND_INEXISTANTE")
	if !strings.HasPrefix(got, "4970") {
		t.Errorf("fallback attendu VISA (prefix 4970), obtenu %q", got)
	}
}
