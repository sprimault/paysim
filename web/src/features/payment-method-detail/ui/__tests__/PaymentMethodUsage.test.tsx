// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PaymentMethodUsage } from '@/features/payment-method-detail/ui/PaymentMethodUsage';
import type { PaymentSummary, SubscriptionOutput } from '@/shared/model';

const fetchPaymentsByToken = vi.hoisted(() => vi.fn());
const fetchSubscriptionsByToken = vi.hoisted(() => vi.fn());

vi.mock('@/entities/payment/api/paymentApi', () => ({ fetchPaymentsByToken }));
vi.mock('@/entities/subscription/api/subscriptionApi', () => ({ fetchSubscriptionsByToken }));

const paiement = (uuid: string, orderId: string, createdAt: string): PaymentSummary => ({
  uuid,
  provider: 'payzen',
  orderId,
  amount: 1000,
  currency: 'EUR',
  state: 'captured',
  createdAt,
  updatedAt: createdAt,
  webhookCount: 0,
});

function rendre(createdAt = '2026-08-01T10:00:01Z') {
  return render(
    <MemoryRouter>
      <PaymentMethodUsage token="tok-1" createdAt={createdAt} />
    </MemoryRouter>,
  );
}

describe('PaymentMethodUsage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fetchPaymentsByToken.mockResolvedValue([]);
    fetchSubscriptionsByToken.mockResolvedValue([]);
  });

  it('annonce qu’un moyen neuf n’a rien payé', async () => {
    rendre();
    await waitFor(() => {
      expect(screen.getByText(/n’a encore rien payé/)).toBeInTheDocument();
    });
  });

  it('liste les paiements et les abonnements liés', async () => {
    fetchPaymentsByToken.mockResolvedValue([
      paiement('u1', 'REGISTER-1', '2026-08-01T10:00:00Z'),
      paiement('u2', 'REPLAY-1', '2026-08-02T10:00:00Z'),
    ]);
    fetchSubscriptionsByToken.mockResolvedValue([
      { id: 's1', provider: 'payzen', paymentMethodToken: 'tok-1',
        amount: 2990, currency: 'EUR', orderId: 'SUB-77',
        cancelled: false } as SubscriptionOutput,
    ]);

    rendre();

    await waitFor(() => expect(screen.getByText('REGISTER-1')).toBeInTheDocument());
    expect(screen.getByText('REPLAY-1')).toBeInTheDocument();
    expect(screen.getByText('SUB-77')).toBeInTheDocument();
    expect(screen.getByText('Paiements (2)')).toBeInTheDocument();
    expect(screen.getByText('Abonnements (1)')).toBeInTheDocument();
  });

  // « D'où vient ce token ? » est la question du débogage : le paiement
  // le plus ancien est celui qui a créé l'alias.
  it('marque le paiement d’enrôlement, et lui seul', async () => {
    fetchPaymentsByToken.mockResolvedValue([
      paiement('u2', 'REPLAY-1', '2026-08-02T10:00:00Z'),
      paiement('u1', 'REGISTER-1', '2026-08-01T10:00:00Z'),
    ]);
    rendre('2026-08-01T10:00:01Z');

    await waitFor(() => expect(screen.getByText('REGISTER-1')).toBeInTheDocument());
    const marques = screen.getAllByText('Enrôlement');
    expect(marques).toHaveLength(1);
    // Le badge doit être dans la ligne de l'enrôlement, pas du rejeu.
    expect(marques[0].closest('a')).toHaveTextContent('REGISTER-1');
  });

  // Si le paiement d'origine a été supprimé, le plus ancien survivant
  // est postérieur à l'alias : le marquer serait faux.
  it('ne marque rien quand l’enrôlement a disparu', async () => {
    fetchPaymentsByToken.mockResolvedValue([
      paiement('u2', 'REPLAY-1', '2026-08-05T10:00:00Z'),
    ]);
    rendre('2026-08-01T10:00:01Z');

    await waitFor(() => expect(screen.getByText('REPLAY-1')).toBeInTheDocument());
    expect(screen.queryByText('Enrôlement')).not.toBeInTheDocument();
  });

  it('n’efface pas la fiche quand la lecture échoue', async () => {
    fetchPaymentsByToken.mockRejectedValue(new Error('indisponible'));
    rendre();
    await waitFor(() => {
      expect(screen.getByText(/n’a encore rien payé/)).toBeInTheDocument();
    });
  });
});
