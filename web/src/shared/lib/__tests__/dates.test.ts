// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { formatShort, humanDuration, formatRelative } from '@/shared/lib/dates';

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

  // La sortie Intl.RelativeTimeFormat peut varier légèrement selon les
  // versions d'ICU. On teste des motifs stables :
  // - < 60s → label justNow injecté tel quel
  // - autres → contient au moins l'unité localisée (minute/heure/…)
  it('rend le label justNow sous 60s (passé ou futur)', () => {
    expect(formatRelative(new Date(ref.getTime() - 30_000), 'fr', "à l'instant", ref)).toBe("à l'instant");
    expect(formatRelative(new Date(ref.getTime() + 30_000), 'fr', "à l'instant", ref)).toBe("à l'instant");
    expect(formatRelative(new Date(ref.getTime() - 30_000), 'en', 'just now', ref)).toBe('just now');
  });

  it.each([
    [120_000, 'fr', 'minute'],
    [3_600_000, 'fr', 'heure'],
    [86_400_000, 'fr', 'jour'],
    [120_000, 'en', 'minute'],
    [3_600_000, 'en', 'hour'],
    [86_400_000, 'en', 'day'],
  ])('offset %dms locale %s contient l\'unité %s', (offsetMs, locale, unit) => {
    const d = new Date(ref.getTime() - offsetMs);
    const out = formatRelative(d, locale as 'fr' | 'en', 'now', ref);
    expect(out.toLowerCase()).toContain(unit);
  });
});
