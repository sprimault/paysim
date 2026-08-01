// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useWebhook, useWebhooksList } from '../useWebhooks';
import { useWebhookStore } from '../webhookStore';
import type { WebhookDetail, WebhookEntry } from '../../../../shared/model';

const originalFetch = globalThis.fetch;

const entry: WebhookEntry = {
  id: 'wh-1',
  url: 'https://x',
  status: 'delivered',
  statusCode: 200,
  attempts: 1,
  createdAt: 't1',
  completedAt: 't1',
};

const detail: WebhookDetail = { ...entry, headers: { X: '1' }, body: 'B' };

describe('useWebhooks', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    useWebhookStore.getState().clear();
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('useWebhooksList fetch au mount si store vide', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify([entry]), { status: 200 }),
    );
    const { result } = renderHook(() => useWebhooksList());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.webhooks).toHaveLength(1);
  });

  it('useWebhook fetch le détail si body absent', async () => {
    useWebhookStore.getState().upsert(entry);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify(detail), { status: 200 }),
    );
    const { result } = renderHook(() => useWebhook('wh-1'));
    await waitFor(() => expect(result.current.webhook?.body).toBe('B'));
  });

  it('useWebhook ne fetch pas si body déjà chargé', () => {
    useWebhookStore.getState().setDetail(detail);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('null', { status: 200 }),
    );
    renderHook(() => useWebhook('wh-1'));
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });
});
