// Copyright (c) 2026 Paysim by Stéphane Primault — Tous droits réservés.
// Author: Stéphane Primault <sprimault@users.noreply.github.com>

package format

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Amount
	}{
		{"0", 0},
		{"0,00", 0},
		{"1", 100},
		{"12", 1200},
		{"12,34", 1234},
		{"12.34", 1234},
		{"12,3", 1230},
		{"12,00", 1200},
		{"1234567", 123456700},
		{"1 234,56", 123456},
		{"1 234,56", 123456},
		{"1 234 567,89", 123456789},
		{"-42,10", -4210},
		{"+7,50", 750},
		{"  12,34  ", 1234},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q) : erreur inattendue %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %d, veut %d", c.in, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		in   string
		want error
	}{
		{"", ErrEmpty},
		{"   ", ErrEmpty},
		{"-", ErrInvalidFormat},
		{"abc", ErrInvalidFormat},
		{"12,", ErrInvalidFormat},
		{",34", ErrInvalidFormat},
		{"12,345", ErrInvalidFormat},
		{"12,3a", ErrInvalidFormat},
		{"12,34,56", ErrInvalidFormat},
		{"12.34.56", ErrInvalidFormat},
		{"12 34,56", ErrInvalidFormat},
		{"1234 567,00", ErrInvalidFormat},
		{"1 23,00", ErrInvalidFormat},
		{"1 2345,00", ErrInvalidFormat},
		{"99999999999999999999", ErrOverflow},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("Parse(%q) : erreur %v, veut %v", c.in, err, c.want)
		}
	}
}

func TestAmountString(t *testing.T) {
	cases := []struct {
		in   Amount
		want string
	}{
		{0, "0,00"},
		{1, "0,01"},
		{9, "0,09"},
		{10, "0,10"},
		{100, "1,00"},
		{1234, "12,34"},
		{123456700, "1234567,00"},
		{-4210, "-42,10"},
		{-9, "-0,09"},
	}
	for _, c := range cases {
		got := c.in.String()
		if got != c.want {
			t.Errorf("Amount(%d).String() = %q, veut %q", int64(c.in), got, c.want)
		}
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	values := []Amount{0, 1, 99, 100, 12345, -12345, 999999999}
	for _, v := range values {
		s := v.String()
		got, err := Parse(s)
		if err != nil {
			t.Errorf("aller-retour %d -> %q : erreur %v", int64(v), s, err)
			continue
		}
		if got != v {
			t.Errorf("aller-retour %d -> %q -> %d", int64(v), s, int64(got))
		}
	}
}
