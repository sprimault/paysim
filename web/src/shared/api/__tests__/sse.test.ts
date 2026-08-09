// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { subscribeSSE, type SSEEvent } from '@/shared/api/sse';

// Mock EventSource — jsdom ne l'implémente pas. Le mock enregistre
// les instances créées pour que les tests puissent déclencher
// onopen/onmessage/onerror manuellement.

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

describe('subscribeSSE', () => {
  beforeEach(() => {
    instances = [];
    // @ts-expect-error — remplace EventSource par un mock minimal.
    globalThis.EventSource = MockEventSource;
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.EventSource = originalEventSource;
  });

  it('ouvre EventSource sur l\'URL préfixée par le base path', () => {
    window.__PAYSIM_BASE_PATH__ = '/paysim';
    subscribeSSE('/paysim/api/v1/events/stream', { onEvent: () => undefined });
    expect(instances).toHaveLength(1);
    expect(instances[0].url).toBe('/paysim/paysim/api/v1/events/stream');
  });

  it('propage onopen vers onStatusChange(true)', () => {
    const changes: boolean[] = [];
    subscribeSSE('/x', { onEvent: () => undefined, onStatusChange: (c) => changes.push(c) });
    instances[0].onopen?.(new Event('open'));
    expect(changes).toEqual([true]);
  });

  it('propage onerror vers onStatusChange(false)', () => {
    const changes: boolean[] = [];
    subscribeSSE('/x', { onEvent: () => undefined, onStatusChange: (c) => changes.push(c) });
    instances[0].onerror?.(new Event('error'));
    expect(changes).toEqual([false]);
  });

  it('parse et transmet l\'événement JSON', () => {
    const received: SSEEvent[] = [];
    subscribeSSE('/x', { onEvent: (e) => received.push(e) });
    const raw = JSON.stringify({
      type: 'payment_created',
      at: '2026-08-01T10:00:00Z',
      data: { uuid: 'abc' },
    });
    instances[0].onmessage?.(new MessageEvent('message', { data: raw }));
    expect(received).toHaveLength(1);
    expect(received[0].type).toBe('payment_created');
  });

  it('ignore silencieusement les payloads mal formés', () => {
    const received: SSEEvent[] = [];
    subscribeSSE('/x', { onEvent: (e) => received.push(e) });
    instances[0].onmessage?.(new MessageEvent('message', { data: '{not json' }));
    expect(received).toHaveLength(0);
  });

  it('close() ferme l\'EventSource', () => {
    const handle = subscribeSSE('/x', { onEvent: () => undefined });
    handle.close();
    expect(instances[0].close).toHaveBeenCalledTimes(1);
  });

  // La première ouverture ne doit rien resynchroniser : les hooks de
  // liste chargent déjà les collections au montage, un refetch de plus
  // serait un doublon à chaque démarrage de l'application.
  it('n\'appelle pas onReconnect à la première ouverture', () => {
    const onReconnect = vi.fn();
    subscribeSSE('/x', { onEvent: () => undefined, onReconnect });
    instances[0].onopen?.(new Event('open'));
    expect(onReconnect).not.toHaveBeenCalled();
  });

  // Le cas que ce mécanisme existe pour couvrir : le serveur redémarre,
  // EventSource se reconnecte seul, l'indicateur repasse au vert — et
  // sans ce signal l'interface continuait d'afficher des entités
  // disparues, le rattrapage par Last-Event-ID ne pouvant rien pour
  // elles.
  it('appelle onReconnect à chaque réouverture, pas la première', () => {
    const onReconnect = vi.fn();
    subscribeSSE('/x', { onEvent: () => undefined, onReconnect });

    instances[0].onopen?.(new Event('open'));
    instances[0].onerror?.(new Event('error'));
    instances[0].onopen?.(new Event('open'));
    expect(onReconnect).toHaveBeenCalledTimes(1);

    instances[0].onerror?.(new Event('error'));
    instances[0].onopen?.(new Event('open'));
    expect(onReconnect).toHaveBeenCalledTimes(2);
  });

  it('reste silencieux sans onReconnect fourni', () => {
    subscribeSSE('/x', { onEvent: () => undefined });
    instances[0].onopen?.(new Event('open'));
    expect(() => instances[0].onopen?.(new Event('open'))).not.toThrow();
  });
});
