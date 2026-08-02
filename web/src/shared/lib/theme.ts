// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Thème utilisateur. `system` = suivre `prefers-color-scheme` — c'est
 * la valeur par défaut, cohérente avec le contrat annoncé dans
 * `.claude/rules/web.md` (« Mode sombre respectant prefers-color-scheme »).
 * `light` / `dark` = surcharge explicite.
 */
export type Theme = 'light' | 'dark' | 'system';

/**
 * Valeur effective après résolution du `system` — c'est ce qui est
 * réellement appliqué à `<html>` (classe `dark` ou non).
 */
export type EffectiveTheme = 'light' | 'dark';

/**
 * Clé localStorage. Préfixée `paysim.` pour éviter les collisions si
 * une autre appli tourne sur le même domaine (peu probable mais gratuit).
 */
export const STORAGE_KEY = 'paysim.theme';

/**
 * Lit le thème enregistré. Retourne `system` par défaut — c'est aussi
 * ce que fait le script inline dans index.html pour rester cohérent
 * entre le boot et le render React.
 */
export function getStoredTheme(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === 'light' || v === 'dark' || v === 'system') return v;
  } catch {
    /* localStorage inaccessible (privacy mode, tests jsdom sans window) */
  }
  return 'system';
}

/**
 * Écrit le thème. Silencieux si localStorage indisponible — le hook
 * useTheme applique quand même à `<html>` pour le run courant.
 */
export function setStoredTheme(theme: Theme): void {
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    /* idem */
  }
}

/**
 * Résout un `Theme` en `EffectiveTheme` — pour `system`, consulte
 * `prefers-color-scheme` du navigateur au moment de l'appel.
 */
export function resolveEffective(theme: Theme): EffectiveTheme {
  if (theme === 'system') {
    return typeof window !== 'undefined' &&
      window.matchMedia?.('(prefers-color-scheme: dark)').matches
      ? 'dark'
      : 'light';
  }
  return theme;
}

/**
 * Applique la classe `dark` sur `<html>`. Idempotent — safe à appeler
 * plusieurs fois. Le script inline dans index.html fait la même chose
 * au boot pour éviter le flash de contenu clair.
 */
export function applyToDocument(effective: EffectiveTheme): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (effective === 'dark') {
    root.classList.add('dark');
  } else {
    root.classList.remove('dark');
  }
}
