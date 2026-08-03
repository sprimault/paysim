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
 * maintenant) localisé selon `locale` : « il y a 5 minutes » en FR,
 * « 5 minutes ago » en EN. Utilise `Intl.RelativeTimeFormat` natif —
 * pas de dépendance.
 *
 * `justNowLabel` couvre le cas < 60s : « à l'instant » / « just now ».
 * `Intl.RelativeTimeFormat.format(0, 'second')` donnerait « dans 0
 * seconde » / « in 0 seconds », ce qu'on veut éviter.
 *
 * Miroir simplifié de `format.FormatRelative` côté Go. À utiliser via
 * le hook `useFormatRelative()` dans un composant React — celui-ci
 * injecte la locale et le label courants sans avoir à les passer
 * partout.
 */
export function formatRelative(
  input: string | Date,
  locale: 'fr' | 'en',
  justNowLabel: string,
  ref: Date = new Date(),
): string {
  const d = input instanceof Date ? input : new Date(input);
  const diffMs = d.getTime() - ref.getTime();
  // Défensif : une date invalide (`new Date("t1")`) produit NaN et
  // ferait crasher Intl.RelativeTimeFormat.format. On préfère un
  // affichage neutre plutôt qu'une erreur remontée à React.
  if (!Number.isFinite(diffMs)) return justNowLabel;
  const abs = Math.abs(diffMs);
  if (abs < 60_000) return justNowLabel;

  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'always' });
  if (abs < 3_600_000) return rtf.format(Math.trunc(diffMs / 60_000), 'minute');
  if (abs < 86_400_000) return rtf.format(Math.trunc(diffMs / 3_600_000), 'hour');
  return rtf.format(Math.trunc(diffMs / 86_400_000), 'day');
}
