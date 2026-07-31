// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Miroir TypeScript des helpers `internal/format/date.go` côté Go.
 * Les noms et contrats reprennent ceux du serveur pour qu'un dev
 * puisse passer de l'un à l'autre sans changer de vocabulaire.
 *
 * Convention : tout en UTC en interne, conversion à l'affichage
 * seulement. Les timestamps reçus de l'API sont des ISO 8601.
 */

/**
 * FormatShort rend un timestamp UTC au format "DD/MM/YYYY HH:mm".
 * Miroir de `format.FormatShort` côté Go.
 */
export function formatShort(input: string | Date): string {
  const d = input instanceof Date ? input : new Date(input);
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${pad(d.getUTCDate())}/${pad(d.getUTCMonth() + 1)}/${d.getUTCFullYear()} ` +
    `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
  );
}

/**
 * HumanDuration rend une durée en français compact : "45ms", "3s",
 * "2min 15s", "1h 23min", "2j 4h". Miroir de `format.HumanDuration`.
 * Prend une durée en millisecondes (choix côté TS car pas de type
 * Duration standard).
 */
export function humanDuration(ms: number): string {
  if (ms === 0) return '0s';
  const neg = ms < 0;
  const abs = Math.abs(ms);

  let out: string;
  if (abs < 1000) {
    out = `${Math.floor(abs)}ms`;
  } else if (abs < 60_000) {
    out = `${Math.floor(abs / 1000)}s`;
  } else if (abs < 3_600_000) {
    const m = Math.floor(abs / 60_000);
    const s = Math.floor((abs % 60_000) / 1000);
    out = s === 0 ? `${m}min` : `${m}min ${s}s`;
  } else if (abs < 86_400_000) {
    const h = Math.floor(abs / 3_600_000);
    const m = Math.floor((abs % 3_600_000) / 60_000);
    out = m === 0 ? `${h}h` : `${h}h ${m}min`;
  } else {
    const j = Math.floor(abs / 86_400_000);
    const h = Math.floor((abs % 86_400_000) / 3_600_000);
    out = h === 0 ? `${j}j` : `${j}j ${h}h`;
  }
  return neg ? `-${out}` : out;
}

/**
 * FormatRelative rend un délai humain entre `input` et `ref` (défaut :
 * maintenant) : "à l'instant", "il y a N minutes/heures/jours", ou
 * "dans N …" quand `input` est postérieur à `ref`.
 */
export function formatRelative(input: string | Date, ref: Date = new Date()): string {
  const d = input instanceof Date ? input : new Date(input);
  const diffMs = ref.getTime() - d.getTime();
  const future = diffMs < 0;
  const abs = Math.abs(diffMs);

  if (abs < 60_000) return 'à l\'instant';

  let qty: number;
  let unit: string;
  if (abs < 3_600_000) {
    qty = Math.floor(abs / 60_000);
    unit = 'minute';
  } else if (abs < 86_400_000) {
    qty = Math.floor(abs / 3_600_000);
    unit = 'heure';
  } else {
    qty = Math.floor(abs / 86_400_000);
    unit = 'jour';
  }
  const plur = qty > 1 ? 's' : '';
  return future ? `dans ${qty} ${unit}${plur}` : `il y a ${qty} ${unit}${plur}`;
}
