// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fetchPayment, fetchPayments, simulatePayment } from '@/entities/payment/api/paymentApi';

const originalFetch = globalThis.fetch;

describe('paymentApi', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('fetchPayments GET /paysim/api/v1/payments', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify([{ uuid: 'a', orderId: 'o', amount: 1, currency: 'EUR', state: 'captured', createdAt: 't', updatedAt: 't' }]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const out = await fetchPayments();
    expect(out).toHaveLength(1);
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/paysim/api/v1/payments');
  });

  it('fetchPayment encode l\'uuid dans le chemin', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ uuid: 'a/b', orderId: 'o', amount: 1, currency: 'EUR', state: 'captured', createdAt: 't', updatedAt: 't', events: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    await fetchPayment('a/b');
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/paysim/api/v1/payments/a%2Fb');
  });

  it('simulatePayment POST avec body JSON', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ deliveryId: 'd', krHash: 'k', channel: 'browserReturn' }), { status: 202, headers: { 'Content-Type': 'application/json' } }),
    );
    const out = await simulatePayment('u1', { outcome: 'PAID' });
    expect(out.deliveryId).toBe('d');
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/paysim/api/v1/payments/u1/simulate');
    expect(call[1].method).toBe('POST');
    expect(call[1].body).toBe('{"outcome":"PAID"}');
  });
});
