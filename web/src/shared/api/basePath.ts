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
 * Racine commune de l'API de contrôle. Portée ici et nulle part
 * ailleurs : elle était recopiée dans cinq constantes `BASE`, et
 * l'oublier ne produit pas une erreur mais une réponse 200 — la SPA
 * répond du HTML sur tout chemin inconnu, si bien que l'appel semble
 * réussir. La version y figure, ce qui donnera un seul endroit à
 * changer le jour d'une v2.
 */
export const API_ROOT = '/paysim/api/v1';

/**
 * apiUrl construit une URL de l'API de contrôle. Le `path` est relatif
 * à la racine de l'API et doit commencer par `/` — `apiUrl('/payments')`
 * rend `/paysim/api/v1/payments`, préfixé du base path quand Paysim est
 * servi sous un sous-chemin.
 */
export function apiUrl(path: string): string {
  return `${getBasePath()}${API_ROOT}${path}`;
}
