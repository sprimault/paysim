// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiGetJson, apiPostJson } from '../../../shared/api/client';
import type { ReplayWebhookResponse, WebhookDetail, WebhookEntry } from '../../../shared/model';

const BASE = '/paysim/api/v1/webhooks';

export function fetchWebhooks(signal?: AbortSignal): Promise<WebhookEntry[]> {
  return apiGetJson<WebhookEntry[]>(BASE, signal);
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
