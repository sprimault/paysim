// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PaymentList } from '@/features/payment-list/ui/PaymentList';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import type { PaymentSummary } from '@/shared/model';

const originalFetch = globalThis.fetch;

const samples: PaymentSummary[] = [
  {
    uuid: 'p1',
  provider: 'payzen',
    orderId: 'CMD-1',
    amount: 4990,
    currency: 'EUR',
    state: 'captured',
    createdAt: 't1',
    updatedAt: 't2',
  },
  {
    uuid: 'p2',
  provider: 'payzen',
    orderId: 'CMD-2',
    amount: 1200,
    currency: 'EUR',
    state: 'declined',
    createdAt: 't1',
    updatedAt: 't3',
  },
];

describe('PaymentList', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    usePaymentStore.getState().clear();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('rend le titre et le compteur quand le store contient des paiements', () => {
    usePaymentStore.getState().setList(samples);
    render(
      <MemoryRouter>
        <PaymentList />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { name: 'Paiements' })).toBeInTheDocument();
    expect(screen.getByText(`${samples.length} paiements`)).toBeInTheDocument();
  });

  it('rend une ligne par paiement du store', () => {
    usePaymentStore.getState().setList(samples);
    render(
      <MemoryRouter>
        <PaymentList />
      </MemoryRouter>,
    );
    for (const p of samples) {
      expect(screen.getByText(p.orderId)).toBeInTheDocument();
    }
  });

  it('affiche EmptyState quand store vide et fetch retourne []', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('[]', { status: 200 }),
    );
    render(
      <MemoryRouter>
        <PaymentList />
      </MemoryRouter>,
    );
    expect(
      await screen.findByRole('heading', { name: 'Aucun paiement' }),
    ).toBeInTheDocument();
  });
});
