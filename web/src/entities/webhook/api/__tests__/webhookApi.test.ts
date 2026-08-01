// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fetchWebhook, fetchWebhooks, replayWebhook } from '@/entities/webhook/api/webhookApi';

const originalFetch = globalThis.fetch;

describe('webhookApi', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('fetchWebhooks GET /paysim/api/v1/webhooks', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify([]), { status: 200 }),
    );
    await fetchWebhooks();
    expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0]).toBe(
      '/paysim/api/v1/webhooks',
    );
  });

  it('fetchWebhook encode l\'id', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ id: 'wh 1', url: '', status: 'delivered', attempts: 1, createdAt: 't', completedAt: 't', headers: {}, body: '' }), { status: 200 }),
    );
    await fetchWebhook('wh 1');
    expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0]).toBe(
      '/paysim/api/v1/webhooks/wh%201',
    );
  });

  it('replayWebhook POST sans body', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ newDeliveryId: 'replay-wh-1-abc' }), { status: 202 }),
    );
    const out = await replayWebhook('wh-1');
    expect(out.newDeliveryId).toBe('replay-wh-1-abc');
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[1].method).toBe('POST');
    expect(call[1].body).toBeUndefined();
  });
});
