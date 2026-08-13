// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import {
  useWebhook,
  useWebhooksList,
  useWebhooksOfPayment,
} from '@/entities/webhook/model/useWebhooks';
import { useWebhookStore } from '@/entities/webhook/model/webhookStore';
import type { WebhookDetail, WebhookEntry } from '@/shared/model';

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
      new Response(JSON.stringify([entry]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const { result } = renderHook(() => useWebhooksList());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.webhooks).toHaveLength(1);
  });

  it('useWebhook fetch le détail si body absent', async () => {
    useWebhookStore.getState().upsert(entry);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const { result } = renderHook(() => useWebhook('wh-1'));
    await waitFor(() => expect(result.current.webhook?.body).toBe('B'));
  });

  it('useWebhook ne fetch pas si body déjà chargé', () => {
    useWebhookStore.getState().setDetail(detail);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('null', { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    renderHook(() => useWebhook('wh-1'));
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  // Le détail d'un paiement affichait le kr-answer du dernier webhook de
  // l'instance, faute de filtre. Ces trois tests verrouillent le
  // comportement qui le corrige.
  it('useWebhooksOfPayment filtre côté serveur', async () => {
    const ofA: WebhookEntry = { ...entry, id: 'wh-a', paymentUuid: 'pay-a' };
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify([ofA]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const { result } = renderHook(() => useWebhooksOfPayment('pay-a'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    const url = String(vi.mocked(globalThis.fetch).mock.calls[0][0]);
    expect(url).toContain('paymentUuid=pay-a');
    expect(result.current.webhooks.map((w) => w.id)).toEqual(['wh-a']);
  });

  // setList remplace tout le store : l'employer ici effacerait les
  // livraisons des autres paiements déjà chargées.
  it('useWebhooksOfPayment ne vide pas le store des autres paiements', async () => {
    const other: WebhookEntry = { ...entry, id: 'wh-autre', paymentUuid: 'pay-b' };
    useWebhookStore.getState().upsert(other);

    const ofA: WebhookEntry = { ...entry, id: 'wh-a', paymentUuid: 'pay-a' };
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify([ofA]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const { result } = renderHook(() => useWebhooksOfPayment('pay-a'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    // Le hook ne remonte que pay-a…
    expect(result.current.webhooks.map((w) => w.id)).toEqual(['wh-a']);
    // …mais l'entrée de l'autre paiement est toujours en store.
    expect(useWebhookStore.getState().webhooks['wh-autre']).toBeDefined();
  });

  it('useWebhooksOfPayment ne fetch pas sans uuid', () => {
    renderHook(() => useWebhooksOfPayment(''));
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });
});
