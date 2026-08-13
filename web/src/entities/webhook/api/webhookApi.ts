// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiGetJson, apiPostJson } from '@/shared/api/client';
import type { ReplayWebhookResponse, WebhookDetail, WebhookEntry } from '@/shared/model';

const BASE = '/webhooks';

export function fetchWebhooks(signal?: AbortSignal): Promise<WebhookEntry[]> {
  return apiGetJson<WebhookEntry[]>(BASE, signal);
}

/**
 * fetchWebhooksOfPayment ne récupère que les livraisons d'un paiement.
 * Le filtre est côté serveur : les webhooks d'un paiement ancien sont
 * sortis de la fenêtre des 200 dernières entrées, et les trier
 * localement afficherait « aucune livraison » là où la base en a.
 */
export function fetchWebhooksOfPayment(
  paymentUuid: string,
  signal?: AbortSignal,
): Promise<WebhookEntry[]> {
  const query = new URLSearchParams({ paymentUuid });
  return apiGetJson<WebhookEntry[]>(`${BASE}?${query.toString()}`, signal);
}

export function fetchWebhook(id: string, signal?: AbortSignal): Promise<WebhookDetail> {
  return apiGetJson<WebhookDetail>(`${BASE}/${encodeURIComponent(id)}`, signal);
}

export function replayWebhook(id: string, signal?: AbortSignal): Promise<ReplayWebhookResponse> {
  return apiPostJson<undefined, ReplayWebhookResponse>(
    `${BASE}/${encodeURIComponent(id)}/replay`,
    undefined,
    signal,
  );
}
