// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { usePaysimEvents } from '@/shared/hooks/usePaysimEvents';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import { useWebhookStore } from '@/entities/webhook/model/webhookStore';

// Mock EventSource local — mêmes patterns que sse/useSSE tests.
interface FakeEventSource {
  url: string;
  close: ReturnType<typeof vi.fn>;
  onopen: ((e: Event) => void) | null;
  onmessage: ((e: MessageEvent<string>) => void) | null;
  onerror: ((e: Event) => void) | null;
}

let instances: FakeEventSource[] = [];

class MockEventSource implements FakeEventSource {
  url: string;
  close = vi.fn();
  onopen: ((e: Event) => void) | null = null;
  onmessage: ((e: MessageEvent<string>) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  constructor(url: string) {
    this.url = url;
    instances.push(this);
  }
}

const originalFetch = globalThis.fetch;
const originalEventSource = globalThis.EventSource;

describe('usePaysimEvents', () => {
  beforeEach(() => {
    instances = [];
    globalThis.fetch = vi.fn();
    // @ts-expect-error — mock minimal
    globalThis.EventSource = MockEventSource;
    usePaymentStore.getState().clear();
    useWebhookStore.getState().clear();
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    globalThis.EventSource = originalEventSource;
  });

  it('ouvre une seule connexion EventSource au montage', () => {
    renderHook(() => usePaysimEvents());
    expect(instances).toHaveLength(1);
  });

  it('reflète le statut connected via onopen', () => {
    const { result } = renderHook(() => usePaysimEvents());
    expect(result.current.connected).toBe(false);
    act(() => instances[0].onopen?.(new Event('open')));
    expect(result.current.connected).toBe(true);
  });

  it('sur payment_created, refetch le détail et met à jour paymentStore', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          uuid: 'p1',
          orderId: 'CMD-1',
          amount: 1000,
          currency: 'EUR',
          state: 'initiated',
          createdAt: 't',
          updatedAt: 't',
          events: [{ at: 't', kind: 'created' }],
        }),
        { status: 200 },
      ),
    );
    renderHook(() => usePaysimEvents());
    act(() => {
      instances[0].onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({
            type: 'payment_created',
            at: 't',
            data: { uuid: 'p1', orderId: 'CMD-1', amount: 1000, currency: 'EUR' },
          }),
        }),
      );
    });
    await waitFor(() =>
      expect(usePaymentStore.getState().payments.p1?.events).toHaveLength(1),
    );
  });

  it('sur webhook_delivered, refetch la liste et met à jour webhookStore', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify([
          {
            id: 'wh-1',
            url: 'https://x',
            status: 'delivered',
            statusCode: 200,
            attempts: 1,
            createdAt: 't',
            completedAt: 't',
          },
        ]),
        { status: 200 },
      ),
    );
    renderHook(() => usePaysimEvents());
    act(() => {
      instances[0].onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({
            type: 'webhook_delivered',
            at: 't',
            data: { id: 'wh-1' },
          }),
        }),
      );
    });
    await waitFor(() =>
      expect(useWebhookStore.getState().webhooks['wh-1']).toBeDefined(),
    );
  });

  it('ignore les events de type inconnu', () => {
    renderHook(() => usePaysimEvents());
    act(() => {
      instances[0].onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({ type: 'unknown', at: 't', data: {} }),
        }),
      );
    });
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  // Resynchronisation au retour de connexion.
  //
  // Le rattrapage par Last-Event-ID ne corrige pas ce qui est déjà
  // affiché : après un redémarrage serveur, le front gardait des
  // paiements disparus et l'indicateur repassait au vert sans que rien
  // ne le contredise. Il fallait cliquer sur Rafraîchir pour voir l'état
  // réel.
  it('relit les collections chargées à la reconnexion', async () => {
    usePaymentStore.getState().setList([]);
    expect(usePaymentStore.getState().listLoaded).toBe(true);

    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue(
      new Response('[]', { status: 200 }),
    );
    renderHook(() => usePaysimEvents());

    act(() => {
      instances[0].onopen?.(new Event('open'));
    });
    expect(globalThis.fetch).not.toHaveBeenCalled();

    act(() => {
      instances[0].onerror?.(new Event('error'));
      instances[0].onopen?.(new Event('open'));
    });
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());

    const urls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.map((c) =>
      String(c[0]),
    );
    expect(urls.some((u) => u.includes('/payments'))).toBe(true);
  });

  // Recharger une collection jamais ouverte ferait travailler l'app pour
  // un écran que personne ne regarde.
  it('ne relit pas les collections jamais chargées', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue(
      new Response('[]', { status: 200 }),
    );
    renderHook(() => usePaysimEvents());

    act(() => {
      instances[0].onopen?.(new Event('open'));
      instances[0].onerror?.(new Event('error'));
      instances[0].onopen?.(new Event('open'));
    });

    await new Promise((r) => setTimeout(r, 20));
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });
});
