// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ApiError, apiGetJson, apiPostJson } from '@/shared/api/client';

// Mock global de fetch — restauré après chaque test.
const originalFetch = globalThis.fetch;

describe('client', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('apiGetJson parse et retourne le JSON', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ a: 1 }), { status: 200 }),
    );
    const out = await apiGetJson<{ a: number }>('/paysim/api/v1/x');
    expect(out).toEqual({ a: 1 });
    expect(globalThis.fetch).toHaveBeenCalledWith('/paysim/api/v1/x', { signal: undefined });
  });

  it('apiGetJson préfixe par le base path quand fourni', async () => {
    window.__PAYSIM_BASE_PATH__ = '/paysim-ext';
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('null', { status: 200 }),
    );
    await apiGetJson('/paysim/api/v1/x');
    expect(globalThis.fetch).toHaveBeenCalledWith('/paysim-ext/paysim/api/v1/x', {
      signal: undefined,
    });
  });

  it('apiGetJson lève ApiError sur status >= 400', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('not found', { status: 404 }),
    );
    let err: unknown;
    try {
      await apiGetJson('/x');
    } catch (e) {
      err = e;
    }
    expect(err).toBeInstanceOf(ApiError);
    const apiErr = err as ApiError;
    expect(apiErr.status).toBe(404);
    expect(apiErr.body).toBe('not found');
  });

  it('apiPostJson envoie le body JSON avec Content-Type', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true }), { status: 202 }),
    );
    const out = await apiPostJson<{ x: number }, { ok: boolean }>('/x', { x: 1 });
    expect(out).toEqual({ ok: true });
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/x');
    expect(call[1].method).toBe('POST');
    expect(call[1].headers).toEqual({ 'Content-Type': 'application/json' });
    expect(call[1].body).toBe('{"x":1}');
  });

  it('apiPostJson accepte un corps vide et retourne undefined si 204', async () => {
    // 204 impose un body null selon la spec Fetch — undici (jsdom)
    // refuse `new Response('', {status: 204})`.
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );
    const out = await apiPostJson('/x', undefined);
    expect(out).toBeUndefined();
  });

  it('apiPostJson retourne undefined si corps vide (non 204)', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('', { status: 200 }),
    );
    const out = await apiPostJson('/x', undefined);
    expect(out).toBeUndefined();
  });
});
