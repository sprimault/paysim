// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach } from 'vitest';
import { useWebhookStore, webhookListSelector } from '../webhookStore';
import type { WebhookDetail, WebhookEntry } from '../../../../shared/model';

const entry = (id: string, createdAt: string): WebhookEntry => ({
  id,
  url: 'https://x',
  status: 'delivered',
  statusCode: 200,
  attempts: 1,
  createdAt,
  completedAt: createdAt,
});

describe('webhookStore', () => {
  beforeEach(() => {
    useWebhookStore.getState().clear();
  });

  it('setList marque listLoaded et remplace', () => {
    useWebhookStore.getState().setList([entry('a', 't')]);
    const s = useWebhookStore.getState();
    expect(Object.keys(s.webhooks)).toEqual(['a']);
    expect(s.listLoaded).toBe(true);
  });

  it('setList préserve body/headers déjà chargés', () => {
    const detail: WebhookDetail = { ...entry('a', 't'), headers: { X: '1' }, body: 'B' };
    useWebhookStore.getState().setDetail(detail);
    useWebhookStore.getState().setList([entry('a', 't2')]);
    const w = useWebhookStore.getState().webhooks.a;
    expect(w.createdAt).toBe('t2');
    expect(w.body).toBe('B');
    expect(w.headers).toEqual({ X: '1' });
  });

  it('remove et upsert', () => {
    useWebhookStore.getState().upsert(entry('a', 't'));
    useWebhookStore.getState().remove('a');
    expect(useWebhookStore.getState().webhooks.a).toBeUndefined();
  });

  it('webhookListSelector trie par createdAt décroissant', () => {
    useWebhookStore.getState().setList([
      entry('old', '2026-08-01T10:00:00Z'),
      entry('new', '2026-08-01T12:00:00Z'),
    ]);
    const list = webhookListSelector(useWebhookStore.getState());
    expect(list.map((w) => w.id)).toEqual(['new', 'old']);
  });
});
