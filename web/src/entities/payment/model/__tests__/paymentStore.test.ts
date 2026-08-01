// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach } from 'vitest';
import { paymentListSelector, usePaymentStore } from '@/entities/payment/model/paymentStore';
import type { PaymentDetail, PaymentSummary } from '@/shared/model';

const summary = (uuid: string, updatedAt: string): PaymentSummary => ({
  uuid,
  provider: 'payzen',
  orderId: `CMD-${uuid}`,
  amount: 1000,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-01T10:00:00Z',
  updatedAt,
});

describe('paymentStore', () => {
  beforeEach(() => {
    usePaymentStore.getState().clear();
  });

  it('setList remplace le contenu et marque listLoaded', () => {
    usePaymentStore.getState().setList([summary('a', '2026-08-01T10:00:00Z')]);
    const s = usePaymentStore.getState();
    expect(Object.keys(s.payments)).toEqual(['a']);
    expect(s.listLoaded).toBe(true);
  });

  it('setList préserve les events déjà chargés pour une entrée existante', () => {
    const detail: PaymentDetail = {
      ...summary('a', '2026-08-01T10:00:00Z'),
      events: [{ at: 't', kind: 'created' }],
    };
    usePaymentStore.getState().setDetail(detail);
    usePaymentStore.getState().setList([summary('a', '2026-08-01T11:00:00Z')]);
    const p = usePaymentStore.getState().payments.a;
    expect(p.updatedAt).toBe('2026-08-01T11:00:00Z');
    expect(p.events).toHaveLength(1);
  });

  it('upsert ajoute ou remplace un paiement', () => {
    usePaymentStore.getState().upsert(summary('a', '2026-08-01T10:00:00Z'));
    usePaymentStore.getState().upsert(summary('a', '2026-08-01T11:00:00Z'));
    expect(usePaymentStore.getState().payments.a.updatedAt).toBe('2026-08-01T11:00:00Z');
  });

  it('remove retire une entrée', () => {
    usePaymentStore.getState().upsert(summary('a', 't'));
    usePaymentStore.getState().remove('a');
    expect(usePaymentStore.getState().payments.a).toBeUndefined();
  });

  it('paymentListSelector trie par updatedAt décroissant', () => {
    usePaymentStore.getState().setList([
      summary('old', '2026-08-01T10:00:00Z'),
      summary('new', '2026-08-01T12:00:00Z'),
      summary('mid', '2026-08-01T11:00:00Z'),
    ]);
    const list = paymentListSelector(usePaymentStore.getState());
    expect(list.map((p) => p.uuid)).toEqual(['new', 'mid', 'old']);
  });
});
