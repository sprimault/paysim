// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Récupération du préfixe de chemin sous lequel Paysim est servi.
 *
 * Le backend Go injecte `window.__PAYSIM_BASE_PATH__` dans index.html
 * juste avant le module React — ce placeholder est résolu au moment
 * où le serveur sert le HTML (contrat A1 validé en 3c). En dev
 * standalone, la variable est absente, on retombe sur "" (chemins
 * relatifs à la racine, le proxy Vite gère la redirection vers
 * localhost:8080).
 *
 * Aucun chemin absolu en dur n'est écrit dans l'app — toute URL API
 * ou SSE passe par cette fonction (invariant web.md).
 */

declare global {
  interface Window {
    __PAYSIM_BASE_PATH__?: string;
  }
}

export function getBasePath(): string {
  if (typeof window !== 'undefined' && typeof window.__PAYSIM_BASE_PATH__ === 'string') {
    return window.__PAYSIM_BASE_PATH__;
  }
  return '';
}

/**
 * apiUrl construit une URL API relative en préfixant le base path.
 * Le `path` doit commencer par `/`.
 */
export function apiUrl(path: string): string {
  return `${getBasePath()}${path}`;
}
