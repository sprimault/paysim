// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { deletePayment, purgePayments } from '@/entities/payment/api/paymentApi';

const originalFetch = globalThis.fetch;

describe('paymentApi delete', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('deletePayment DELETE /paysim/api/v1/payments/{uuid}', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );
    await deletePayment('u1');
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/paysim/api/v1/payments/u1');
    expect(call[1].method).toBe('DELETE');
  });

  it('purgePayments sans provider DELETE /paysim/api/v1/payments', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ deleted: 3 }), { status: 200 }),
    );
    const out = await purgePayments();
    expect(out.deleted).toBe(3);
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/paysim/api/v1/payments');
  });

  it('purgePayments avec provider passe ?provider=X', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ deleted: 2 }), { status: 200 }),
    );
    await purgePayments('payzen');
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/paysim/api/v1/payments?provider=payzen');
  });

  it('encode l\'uuid dans le chemin de deletePayment', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );
    await deletePayment('a/b');
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/paysim/api/v1/payments/a%2Fb');
  });
});
