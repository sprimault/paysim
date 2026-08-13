// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { usePayment, usePaymentsList } from '@/entities/payment/model/usePayments';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import type { PaymentDetail, PaymentSummary } from '@/shared/model';

const originalFetch = globalThis.fetch;

const summary: PaymentSummary = {
  uuid: 'p1',
  provider: 'payzen',
  orderId: 'CMD-1',
  amount: 1000,
  currency: 'EUR',
  state: 'captured',
  createdAt: 't1',
  updatedAt: 't2',
  webhookCount: 0,
  webhookReplayCount: 0,
};

const detail: PaymentDetail = { ...summary, events: [{ at: 't', kind: 'created' }] };

describe('usePayments', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    usePaymentStore.getState().clear();
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('usePaymentsList déclenche fetch au mount si store vide', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify([summary]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const { result } = renderHook(() => usePaymentsList());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.payments).toHaveLength(1);
    expect(result.current.payments[0].uuid).toBe('p1');
  });

  it('usePaymentsList refetch au mount même si le store est peuplé (SWR silencieux)', async () => {
    // Contrat : le hook refetch toujours au mount pour repartir de la
    // vérité serveur ; il ne bascule pas en `loading:true` si le store
    // a déjà des données (pas de skeleton pendant la mise à jour).
    usePaymentStore.getState().setList([summary]);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify([summary]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const { result } = renderHook(() => usePaymentsList());
    expect(result.current.loading).toBe(false);
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1));
  });

  it('usePayment fetch le détail si events absent', async () => {
    usePaymentStore.getState().upsert(summary);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify(detail), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    const { result } = renderHook(() => usePayment('p1'));
    await waitFor(() => expect(result.current.payment?.events).toHaveLength(1));
    expect(result.current.loading).toBe(false);
  });

  it('usePayment ne fetch pas si events déjà chargé', () => {
    usePaymentStore.getState().setDetail(detail);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('null', { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    renderHook(() => usePayment('p1'));
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('usePayment expose error si le fetch échoue', async () => {
    usePaymentStore.getState().upsert(summary);
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('nope', { status: 500 }),
    );
    const { result } = renderHook(() => usePayment('p1'));
    await waitFor(() => expect(result.current.error).toBeDefined());
  });
});
