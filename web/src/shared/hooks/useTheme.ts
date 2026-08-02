// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';
import {
  applyToDocument,
  getStoredTheme,
  resolveEffective,
  setStoredTheme,
  type EffectiveTheme,
  type Theme,
} from '@/shared/lib/theme';

/**
 * Hook de gestion du thème utilisateur. Retourne le `theme` demandé
 * (light/dark/system), l'`effective` réellement appliqué (light/dark
 * après résolution du system), et un setter qui persiste + applique.
 *
 * Quand `theme === 'system'`, un listener sur `prefers-color-scheme`
 * met à jour l'effective à la volée si l'utilisateur bascule son OS
 * en cours de session — pas de reload nécessaire.
 */
export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(() => getStoredTheme());
  const [effective, setEffective] = useState<EffectiveTheme>(() =>
    resolveEffective(getStoredTheme()),
  );

  const setTheme = useCallback((next: Theme) => {
    setStoredTheme(next);
    setThemeState(next);
    const eff = resolveEffective(next);
    setEffective(eff);
    applyToDocument(eff);
  }, []);

  // Suit prefers-color-scheme uniquement en mode `system`. En mode
  // light/dark forcés, l'utilisateur a explicitement choisi, on ne
  // touche pas.
  useEffect(() => {
    if (theme !== 'system') return;
    if (typeof window === 'undefined' || !window.matchMedia) return;

    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = () => {
      const eff: EffectiveTheme = mql.matches ? 'dark' : 'light';
      setEffective(eff);
      applyToDocument(eff);
    };
    mql.addEventListener('change', handler);
    return () => mql.removeEventListener('change', handler);
  }, [theme]);

  return { theme, effective, setTheme };
}
