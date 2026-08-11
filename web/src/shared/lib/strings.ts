// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Miroir TypeScript des helpers `internal/format/text.go` côté Go.
 */

/**
 * Truncate coupe une chaîne à `max` caractères (Unicode code points)
 * en préservant les frontières UTF-16 sûres. Ajoute une ellipse
 * Unicode (…) en suffixe si la chaîne est effectivement tronquée.
 * `max` s'entend hors ellipse.
 */
export function truncate(s: string, max: number): string {
  if (max < 0) max = 0;
  const chars = [...s]; // découpe par code point (émojis, accents composés)
  if (chars.length <= max) return s;
  return chars.slice(0, max).join('') + '…';
}

/**
 * matchesSearch dit si l'un des champs contient la recherche.
 *
 * Correspondance partielle et insensible à la casse : on colle un bout
 * d'UUID lu dans un log, ou on tape « cmd-10 » pour retrouver une série
 * de commandes. Exiger l'égalité obligerait à connaître la valeur
 * exacte, ce qu'on cherche justement.
 *
 * Une recherche vide laisse tout passer — c'est l'absence de filtre, pas
 * un filtre qui ne trouve rien.
 *
 * Les champs absents sont ignorés plutôt que traités comme des chaînes
 * vides : un paiement sans token ne doit pas ressortir sur une recherche
 * qui ne le concerne pas.
 */
export function matchesSearch(query: string, ...fields: (string | undefined)[]): boolean {
  const q = query.trim().toLowerCase();
  if (q === '') return true;
  return fields.some((f) => f !== undefined && f.toLowerCase().includes(q));
}

/**
 * Mask masque le milieu d'une chaîne en gardant `prefix` caractères
 * en tête et `suffix` en queue ; le milieu est remplacé par des
 * étoiles. Si prefix+suffix+3 dépasse la longueur, la totalité est
 * masquée par "***" — évite d'exposer la quasi-totalité d'un secret
 * court sous couvert de « masquage ».
 */
export function mask(s: string, prefix: number, suffix: number): string {
  if (prefix < 0) prefix = 0;
  if (suffix < 0) suffix = 0;
  const chars = [...s];
  if (prefix + suffix + 3 > chars.length) return '***';
  const middle = chars.length - prefix - suffix;
  return (
    chars.slice(0, prefix).join('') +
    '*'.repeat(middle) +
    chars.slice(chars.length - suffix).join('')
  );
}
