// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { Header } from '@/widgets/header/Header';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import { usePaymentMethodStore } from '@/entities/payment-method/model/paymentMethodStore';
import { useSubscriptionStore } from '@/entities/subscription/model/subscriptionStore';
import type { PaymentMethodOutput, PaymentSummary, SubscriptionOutput } from '@/shared/model';

// Le Header amorce les trois collections au montage. Sans ces doubles,
// chaque test partirait chercher un serveur qui n'existe pas.
const fetchPayments = vi.hoisted(() => vi.fn());
const fetchSubscriptions = vi.hoisted(() => vi.fn());
const fetchPaymentMethods = vi.hoisted(() => vi.fn());

vi.mock('@/entities/payment/api/paymentApi', () => ({ fetchPayments }));
vi.mock('@/entities/subscription/api/subscriptionApi', () => ({ fetchSubscriptions }));
vi.mock('@/entities/payment-method/api/paymentMethodApi', () => ({ fetchPaymentMethods }));

const paiement = (uuid: string): PaymentSummary => ({
  uuid,
  provider: 'payzen',
  orderId: `CMD-${uuid}`,
  amount: 1000,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-01T10:00:00Z',
  updatedAt: '2026-08-01T10:00:00Z',
  webhookCount: 0,
  webhookReplayCount: 0,
});

function renderHeader() {
  return render(
    <MemoryRouter>
      <Header />
    </MemoryRouter>,
  );
}

describe('pastilles de navigation', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fetchPayments.mockResolvedValue([]);
    fetchSubscriptions.mockResolvedValue([]);
    fetchPaymentMethods.mockResolvedValue([]);
    usePaymentStore.getState().clear();
    useSubscriptionStore.getState().clear();
    usePaymentMethodStore.getState().clear();
  });

  it('n’affiche aucune pastille quand tout est vide', async () => {
    renderHeader();
    await waitFor(() => expect(fetchPayments).toHaveBeenCalled());

    const lien = screen.getByRole('link', { name: /paiements/i });
    expect(lien.textContent).toBe('Paiements');
  });

  it('affiche le décompte de chaque collection', async () => {
    fetchPayments.mockResolvedValue([paiement('a'), paiement('b'), paiement('c')]);
    fetchSubscriptions.mockResolvedValue([
      { id: 's1', createdAt: '2026-08-01T10:00:00Z' } as SubscriptionOutput,
    ]);
    fetchPaymentMethods.mockResolvedValue([
      { token: 't1' } as PaymentMethodOutput,
      { token: 't2' } as PaymentMethodOutput,
    ]);

    renderHeader();

    await waitFor(() => {
      expect(screen.getByRole('link', { name: /paiements/i }).textContent).toBe('Paiements3');
    });
    expect(screen.getByRole('link', { name: /abonnements/i }).textContent).toBe('Abonnements1');
    expect(screen.getByRole('link', { name: /moyens de paiement/i }).textContent).toBe(
      'Moyens de paiement2',
    );
  });

  it('n’amorce pas une collection déjà chargée', async () => {
    usePaymentStore.getState().setList([paiement('a')]);
    renderHeader();

    await waitFor(() => expect(fetchSubscriptions).toHaveBeenCalled());
    expect(fetchPayments).not.toHaveBeenCalled();
  });

  it('reste affiché malgré l’échec d’un amorçage', async () => {
    fetchSubscriptions.mockRejectedValue(new Error('indisponible'));
    renderHeader();

    await waitFor(() => expect(fetchSubscriptions).toHaveBeenCalled());
    expect(screen.getByRole('link', { name: /abonnements/i })).toBeInTheDocument();
  });
});
