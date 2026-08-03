// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import type { Lang } from './messages';

const STORAGE_KEY = 'paysim-lang';

/**
 * Store i18n. La langue courante est persistée dans localStorage
 * (mêmes conventions que le theme store).
 *
 * L'initialisation se fait via detectInitialLang() côté init.ts, pas
 * ici, pour garder le store pur (pas d'accès window au chargement du
 * module — évite les erreurs SSR-like en tests).
 */
interface LangState {
  lang: Lang;
  setLang: (lang: Lang) => void;
}

export const useLangStore = create<LangState>((set) => ({
  lang: 'fr',
  setLang: (lang) => {
    try {
      window.localStorage.setItem(STORAGE_KEY, lang);
    } catch {
      // localStorage indisponible (mode privé, quotas) — on continue
      // avec la valeur en mémoire, non persistée. Pas critique.
    }
    if (typeof document !== 'undefined') {
      document.documentElement.lang = lang;
    }
    set({ lang });
  },
}));

/** Clé localStorage exportée pour les tests et le script inline
 *  d'init dans index.html. */
export const LANG_STORAGE_KEY = STORAGE_KEY;
