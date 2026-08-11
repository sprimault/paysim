// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useSSE } from '@/shared/hooks/useSSE';
import type { SSEEvent } from '@/shared/api/sse';

// Même mock EventSource que sse.test.ts — dupliqué ici pour ne pas
// coupler les fichiers de test. Un mock global dans setup.ts serait
// une alternative si le pattern se répète.

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

const originalEventSource = globalThis.EventSource;

describe('useSSE', () => {
  beforeEach(() => {
    instances = [];
    // @ts-expect-error — remplace EventSource par un mock minimal.
    globalThis.EventSource = MockEventSource;
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.EventSource = originalEventSource;
  });

  it('connecté = false initialement, passe à true sur onopen', () => {
    const { result } = renderHook(() => useSSE('/stream', () => undefined));
    expect(result.current.connected).toBe(false);
    act(() => {
      instances[0].onopen?.(new Event('open'));
    });
    expect(result.current.connected).toBe(true);
  });

  it('appelle onEvent avec le payload parsé', () => {
    const received: SSEEvent[] = [];
    renderHook(() => useSSE('/stream', (e) => received.push(e)));
    act(() => {
      instances[0].onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({ type: 't', at: '2026-08-01', data: 42 }),
        }),
      );
    });
    expect(received).toHaveLength(1);
    expect(received[0].data).toBe(42);
  });

  it('ferme l\'EventSource au démontage', () => {
    const { unmount } = renderHook(() => useSSE('/stream', () => undefined));
    unmount();
    expect(instances[0].close).toHaveBeenCalledTimes(1);
  });

  // Sur une instance au repos, aucun événement ne viendra jamais : sans
  // l'ouverture comme premier signe de vie, le témoin de fraîcheur
  // resterait muet et laisserait croire à une panne.
  it('horodate l\'ouverture du flux comme un signe de vie', () => {
    const { result } = renderHook(() => useSSE('/stream', () => undefined));
    expect(result.current.lastEventAt).toBeUndefined();
    act(() => {
      instances[0].onopen?.(new Event('open'));
    });
    expect(result.current.lastEventAt).toBeGreaterThan(0);
  });

  it('horodate chaque événement reçu', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-08-11T10:00:00Z'));
    try {
      const { result } = renderHook(() => useSSE('/stream', () => undefined));
      act(() => {
        instances[0].onopen?.(new Event('open'));
      });
      const ouverture = result.current.lastEventAt;
      vi.setSystemTime(new Date('2026-08-11T10:00:30Z'));
      act(() => {
        instances[0].onmessage?.(
          new MessageEvent('message', { data: JSON.stringify({ type: 't', at: 'x', data: 1 }) }),
        );
      });
      expect(result.current.lastEventAt).toBe((ouverture ?? 0) + 30_000);
    } finally {
      vi.useRealTimers();
    }
  });

  it('ne rouvre pas la connexion à chaque re-render (callback stocké en ref)', () => {
    let cb = () => undefined;
    const { rerender } = renderHook(({ fn }: { fn: () => void }) => useSSE('/stream', fn), {
      initialProps: { fn: cb },
    });
    cb = () => undefined; // nouvelle fonction identity
    rerender({ fn: cb });
    // path inchangé → toujours 1 seule instance créée
    expect(instances).toHaveLength(1);
  });
});
