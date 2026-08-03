// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { LANG_STORAGE_KEY, useLangStore } from './store';
import type { Lang } from './messages';

/**
 * Détermine la langue à utiliser au premier chargement, dans cet
 * ordre :
 *   1. Valeur persistée dans localStorage (`paysim-lang`).
 *   2. Détection navigator.language : si commence par « en- », EN ;
 *      sinon FR (défaut voulu — audience initialement francophone).
 *
 * Ne dépend d'aucun composant, se lance à l'entrée de main.tsx.
 */
export function detectInitialLang(): Lang {
  try {
    const stored = window.localStorage.getItem(LANG_STORAGE_KEY);
    if (stored === 'fr' || stored === 'en') return stored;
  } catch {
    // localStorage inaccessible, on continue avec la détection navigator
  }
  if (typeof navigator !== 'undefined' && navigator.language?.toLowerCase().startsWith('en')) {
    return 'en';
  }
  return 'fr';
}

/**
 * initI18n applique la langue détectée au store et à l'attribut
 * lang du document. À appeler une fois au démarrage, avant le
 * premier render.
 */
export function initI18n(): void {
  const lang = detectInitialLang();
  useLangStore.getState().setLang(lang);
}
