// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { messages, type MessageKey } from './messages';
import { useLangStore } from './store';

/**
 * useT retourne une fonction `t(key)` qui résout une clé dans le
 * dictionnaire selon la langue courante. Re-render automatique sur
 * changement de langue via l'abonnement Zustand.
 *
 * Usage :
 *   const t = useT();
 *   return <span>{t('header.nav.payments')}</span>;
 *
 * La clé est typée : une clé absente du dictionnaire fait échouer
 * le build TypeScript, pas seulement le runtime.
 */
export function useT(): (key: MessageKey) => string {
  const lang = useLangStore((s) => s.lang);
  return (key: MessageKey) => messages[lang][key];
}
