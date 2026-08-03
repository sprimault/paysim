// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { formatRelative } from '@/shared/lib/dates';
import { useLangStore } from '@/shared/i18n/store';
import { useT } from '@/shared/i18n/useT';

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
 */
export function useFormatRelative(): (input: string | Date, ref?: Date) => string {
  const lang = useLangStore((s) => s.lang);
  const t = useT();
  const justNow = t('common.relative.now');
  return (input, ref) => formatRelative(input, lang, justNow, ref);
}
