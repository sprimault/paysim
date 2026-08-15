// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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
    webhookCount: 0,
    webhookReplayCount: 0,
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
    webhookCount: 0,
    webhookReplayCount: 0,
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

  // Le bouton dépendait de la liste affichée, alors que la purge ne
  // connaît que la marque : une recherche sans résultat le faisait
  // disparaître, et trois lignes filtrées annonçaient « vider » pour
  // cinquante suppressions.
  it('garde le bouton de purge quand une recherche ne trouve rien', async () => {
    const user = userEvent.setup();
    usePaymentStore.getState().setList(samples);
    render(
      <MemoryRouter>
        <PaymentList />
      </MemoryRouter>,
    );
    const bouton = screen.getByRole('button', { name: /vider les paiements/i });
    expect(bouton).toBeEnabled();

    await user.type(screen.getByPlaceholderText(/Commande/), 'introuvable-xyz');
    expect(screen.getByRole('button', { name: /vider les paiements/i })).toBeEnabled();
  });

  it('désactive le bouton de purge quand la marque n’a aucun paiement', () => {
    usePaymentStore.getState().setList([]);
    render(
      <MemoryRouter>
        <PaymentList />
      </MemoryRouter>,
    );
    expect(screen.getByRole('button', { name: /vider les paiements/i })).toBeDisabled();
  });

  it('affiche EmptyState quand store vide et fetch retourne []', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } }),
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
