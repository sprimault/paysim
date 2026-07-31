// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Miroir TypeScript des helpers `internal/format/number.go` côté Go.
 */

// Espace insécable U+00A0 — cohérent avec ce que money.Parse accepte
// côté Go. Même caractère aux deux extrémités du protocole. Écrit en
// séquence d'échappement pour éviter tout piège d'encodage source.
const NBSP = ' ';

/**
 * FormatInt formate un entier signé avec séparateurs de milliers en
 * espace insécable, convention française. `1234567` devient
 * `"1 234 567"` (chaque séparateur = U+00A0).
 *
 * Les entiers de trois chiffres ou moins passent tels quels.
 */
export function formatInt(n: number): string {
  const neg = n < 0;
  const abs = Math.abs(Math.trunc(n));
  const raw = abs.toString();
  if (raw.length <= 3) return neg ? `-${raw}` : raw;

  const first = raw.length % 3 || 3;
  const groups: string[] = [raw.slice(0, first)];
  for (let i = first; i < raw.length; i += 3) {
    groups.push(raw.slice(i, i + 3));
  }
  const joined = groups.join(NBSP);
  return neg ? `-${joined}` : joined;
}

/**
 * FormatAmount formate un montant en centimes vers "12,34" (virgule
 * décimale FR, deux décimales toujours). Sans séparateur de milliers
 * ni symbole devise — ces choix relèvent de l'appelant, cohérent avec
 * `Amount.String()` côté Go.
 */
export function formatAmount(cents: number): string {
  const neg = cents < 0;
  const abs = Math.abs(Math.trunc(cents));
  const units = Math.floor(abs / 100);
  const rem = abs % 100;
  const decimals = rem < 10 ? `0${rem}` : `${rem}`;
  return `${neg ? '-' : ''}${units},${decimals}`;
}
