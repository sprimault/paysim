// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { estDecalee, SEUIL_DECALAGE_MS, useClockStore } from '@/shared/model/clockStore';
import * as client from '@/shared/api/client';

describe('estDecalee', () => {
  it('ignore le bruit sous le seuil', () => {
    expect(estDecalee(0)).toBe(false);
    expect(estDecalee(SEUIL_DECALAGE_MS - 1)).toBe(false);
    expect(estDecalee(-(SEUIL_DECALAGE_MS - 1))).toBe(false);
  });

  it('signale une avance délibérée', () => {
    expect(estDecalee(SEUIL_DECALAGE_MS)).toBe(true);
    expect(estDecalee(96 * 3600 * 1000)).toBe(true);
  });
});

describe('useClockStore', () => {
  beforeEach(() => {
    useClockStore.setState({ decalageMs: 0, charge: false });
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('déduit le décalage de la réponse serveur', async () => {
    vi.spyOn(client, 'apiGetJson').mockResolvedValue({
      now: '2026-01-05T00:00:00Z',
      offset: '96h0m0s',
      offsetSeconds: 345600,
    });
    await useClockStore.getState().rafraichir();
    expect(useClockStore.getState().decalageMs).toBe(96 * 3600 * 1000);
    expect(useClockStore.getState().charge).toBe(true);
  });

  // now() se recalcule à l'appel : l'affichage doit continuer de
  // vieillir sans qu'on réinterroge le serveur.
  it('fait avancer now() avec le temps qui passe', async () => {
    vi.spyOn(client, 'apiGetJson').mockResolvedValue({
      now: '2026-01-02T00:00:00Z',
      offset: '24h0m0s',
      offsetSeconds: 86400,
    });
    await useClockStore.getState().rafraichir();
    const premier = useClockStore.getState().now().getTime();
    vi.advanceTimersByTime(60_000);
    expect(useClockStore.getState().now().getTime() - premier).toBe(60_000);
  });

  it('recharge le décalage après une avance', async () => {
    const post = vi.spyOn(client, 'apiPostJson').mockResolvedValue({
      now: '2026-01-08T00:00:00Z',
      offset: '168h0m0s',
      offsetSeconds: 604800,
    });
    await useClockStore.getState().avancer('168h');
    expect(post).toHaveBeenCalledWith('/clock/advance', { duration: '168h' });
    expect(useClockStore.getState().decalageMs).toBe(168 * 3600 * 1000);
  });

  it('ramène le décalage à zéro après réinitialisation', async () => {
    useClockStore.setState({ decalageMs: 999_999, charge: true });
    vi.spyOn(client, 'apiPostJson').mockResolvedValue({
      now: '2026-01-01T00:00:00Z',
      offset: '0s',
      offsetSeconds: 0,
    });
    await useClockStore.getState().reinitialiser();
    expect(useClockStore.getState().decalageMs).toBe(0);
  });
});
