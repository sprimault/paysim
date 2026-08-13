// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiUrl } from './basePath';
import { translate } from '@/shared/i18n/useT';

/**
 * Client HTTP minimaliste pour l'API Paysim. Toutes les URL passent
 * par `apiUrl()` — jamais de chemin absolu (invariant web.md).
 *
 * L'auth Bearer est prévue (PAYSIM_API_TOKEN côté serveur) mais laissée
 * hors du client v1 : en dev PAYSIM_API_TOKEN est vide, l'API est
 * ouverte. Quand elle deviendra utile, on injectera le token via
 * `window.__PAYSIM_TOKEN__` sur le même modèle que basePath.
 */

/**
 * ApiError enrobe une réponse HTTP >= 400. Le status et le corps brut
 * sont exposés pour permettre à l'UI d'afficher un message contextuel
 * (toast, empty state).
 */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: string,
    message?: string,
  ) {
    super(message ?? `HTTP ${status}`);
    this.name = 'ApiError';
  }
}

async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  const url = apiUrl(path);
  const resp = await fetch(url, init);
  if (!resp.ok) {
    let body = '';
    try {
      body = await resp.text();
    } catch {
      // Si le corps n'est pas lisible, on garde message vide.
    }
    throw new ApiError(resp.status, body);
  }
  return resp;
}

/**
 * exigerJson refuse une réponse qui n'est pas du JSON.
 *
 * Un chemin mal formé n'échoue pas côté serveur : la SPA répond 200
 * avec son index.html sur tout ce qu'elle ne connaît pas. Sans ce
 * contrôle, l'appel « réussit » et ne casse qu'au décodage, avec un
 * message qui ne dit rien de la cause — ou pire, il ne casse pas du
 * tout. Vu en vrai : trois routes d'horloge appelées sans leur préfixe
 * d'API, un bouton sans effet et aucune erreur.
 */
function exigerJson(resp: Response, path: string): void {
  const type = resp.headers.get('Content-Type') ?? '';
  if (type.includes('json')) return;
  throw new ApiError(
    resp.status,
    type,
    translate('api.notJson', { path, type: type || translate('api.noContentType') }),
  );
}

async function lireJson<T>(resp: Response, path: string): Promise<T> {
  exigerJson(resp, path);
  return (await resp.json()) as T;
}

/**
 * apiGetJson exécute un GET et parse la réponse en JSON typé. La
 * cancellation via `signal` est propagée jusqu'à fetch — utile depuis
 * useEffect pour annuler quand le composant démonte.
 */
export async function apiGetJson<T>(path: string, signal?: AbortSignal): Promise<T> {
  const resp = await apiFetch(path, { signal });
  return lireJson<T>(resp, path);
}

/**
 * apiPostJson envoie un JSON (ou body vide si `body === undefined`)
 * et parse la réponse. Content-Type défini automatiquement quand un
 * corps est présent.
 */
export async function apiPostJson<TReq, TRes>(
  path: string,
  body: TReq,
  signal?: AbortSignal,
): Promise<TRes> {
  const init: RequestInit = { method: 'POST', signal };
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(body);
  }
  const resp = await apiFetch(path, init);
  // 202 Accepted / 204 No Content peuvent renvoyer un corps vide.
  if (resp.status === 204) {
    return undefined as TRes;
  }
  const text = await resp.text();
  if (text.length === 0) {
    return undefined as TRes;
  }
  exigerJson(resp, path);
  return JSON.parse(text) as TRes;
}

/**
 * apiDelete envoie un DELETE et parse la réponse JSON si présente.
 * Retourne undefined sur 204 No Content (delete unitaire) ou corps
 * vide ; sinon parse le JSON (bulk delete renvoie `{deleted: n}`).
 */
export async function apiDelete<TRes>(path: string, signal?: AbortSignal): Promise<TRes> {
  const resp = await apiFetch(path, { method: 'DELETE', signal });
  if (resp.status === 204) {
    return undefined as TRes;
  }
  const text = await resp.text();
  if (text.length === 0) {
    return undefined as TRes;
  }
  exigerJson(resp, path);
  return JSON.parse(text) as TRes;
}
