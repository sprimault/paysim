// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { formatInt, formatAmount } from '../numbers';

// Espace insécable U+00A0 — écrit en séquence d'échappement pour
// éviter tout piège d'encodage source.
const NBSP = ' ';

describe('formatInt', () => {
  it.each([
    [0, '0'],
    [5, '5'],
    [999, '999'],
    [1000, `1${NBSP}000`],
    [1234, `1${NBSP}234`],
    [12345, `12${NBSP}345`],
    [123456, `123${NBSP}456`],
    [1234567, `1${NBSP}234${NBSP}567`],
    [1000000, `1${NBSP}000${NBSP}000`],
    [-1, '-1'],
    [-1234, `-1${NBSP}234`],
    [-1234567, `-1${NBSP}234${NBSP}567`],
  ])('formatInt(%d) = %j', (n, expected) => {
    expect(formatInt(n)).toBe(expected);
  });
});

describe('formatAmount', () => {
  it.each([
    [0, '0,00'],
    [1, '0,01'],
    [9, '0,09'],
    [10, '0,10'],
    [100, '1,00'],
    [1234, '12,34'],
    [123456700, '1234567,00'],
    [-4210, '-42,10'],
    [-9, '-0,09'],
  ])('formatAmount(%d) = %j', (cents, expected) => {
    expect(formatAmount(cents)).toBe(expected);
  });
});
