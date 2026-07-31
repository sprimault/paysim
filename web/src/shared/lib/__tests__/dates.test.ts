// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { formatShort, humanDuration, formatRelative } from '../dates';

describe('formatShort', () => {
  it('rend un timestamp UTC au format DD/MM/YYYY HH:mm', () => {
    // 12 mars 2026 14:23:45 UTC.
    const d = new Date(Date.UTC(2026, 2, 12, 14, 23, 45));
    expect(formatShort(d)).toBe('12/03/2026 14:23');
  });

  it('accepte une chaîne ISO 8601', () => {
    expect(formatShort('2026-03-12T14:23:45Z')).toBe('12/03/2026 14:23');
  });
});

describe('humanDuration', () => {
  it.each([
    [0, '0s'],
    [45, '45ms'],
    [999, '999ms'],
    [1000, '1s'],
    [59_000, '59s'],
    [60_000, '1min'],
    [135_000, '2min 15s'],
    [3_600_000, '1h'],
    [4_980_000, '1h 23min'],
    [86_400_000, '1j'],
    [187_200_000, '2j 4h'],
    [-5000, '-5s'],
  ])('humanDuration(%d) = %s', (ms, expected) => {
    expect(humanDuration(ms)).toBe(expected);
  });
});

describe('formatRelative', () => {
  const ref = new Date(Date.UTC(2026, 2, 12, 14, 0, 0));

  it.each([
    [30_000, "à l'instant"], // 30s passé
    [-30_000, "à l'instant"], // 30s futur
    [60_000, 'il y a 1 minute'],
    [120_000, 'il y a 2 minutes'],
    [3_600_000, 'il y a 1 heure'],
    [10_800_000, 'il y a 3 heures'],
    [86_400_000, 'il y a 1 jour'],
    [-120_000, 'dans 2 minutes'],
  ])('offset %dms = %s', (offsetMs, expected) => {
    const d = new Date(ref.getTime() - offsetMs);
    expect(formatRelative(d, ref)).toBe(expected);
  });
});
