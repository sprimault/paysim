// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { formatRelative } from '@/shared/lib/dates';
import { useLangStore } from '@/shared/i18n/store';
import { useT } from '@/shared/i18n/useT';
import { useClockStore } from '@/shared/model/clockStore';

/**
 * useFormatRelative retourne une fonction `(input, ref?) => string`
 * qui localise l'affichage relatif selon la langue courante du store.
 *
 * Usage :
 *   const rel = useFormatRelative();
 *   <span>{rel(payment.createdAt)}</span>
 *
 * Utilise Intl.RelativeTimeFormat natif — zéro dépendance, sortie
 * localisée par le navigateur.
 *
 * La référence par défaut est l'heure du **simulateur**, pas celle du
 * navigateur. Sur une instance avancée de quatre jours, la seconde
 * afficherait « créé dans 4 jours » sur chaque paiement : plausible,
 * faux, et sans rien pour l'expliquer.
 */
export function useFormatRelative(): (input: string | Date, ref?: Date) => string {
  const lang = useLangStore((s) => s.lang);
  const t = useT();
  const justNow = t('common.relative.now');
  const decalageMs = useClockStore((s) => s.decalageMs);
  return (input, ref) =>
    formatRelative(input, lang, justNow, ref ?? new Date(Date.now() + decalageMs));
}
