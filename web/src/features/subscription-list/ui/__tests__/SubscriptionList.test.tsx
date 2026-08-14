// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SubscriptionList } from '@/features/subscription-list/ui/SubscriptionList';
import { useSubscriptionStore } from '@/entities/subscription/model/subscriptionStore';
import type { SubscriptionOutput } from '@/shared/model';

function makeSub(overrides: Partial<SubscriptionOutput> = {}): SubscriptionOutput {
  return {
    id: 'sub-1',
    provider: 'payzen',
    paymentMethodToken: 'pmt-x',
    amount: 2990,
    currency: 'EUR',
    orderId: 'SUB-42',
    effectDate: '2026-09-01',
    rrule: 'RRULE:FREQ=MONTHLY;INTERVAL=1',
    cancelled: false,
    billingCount: 0,
    createdAt: '2026-08-02T10:00:00Z',
    ...overrides,
  };
}

describe('<SubscriptionList />', () => {
  beforeEach(() => {
    useSubscriptionStore.setState({ subscriptions: {}, listLoaded: true });
    vi.spyOn(global, 'fetch').mockImplementation(async () => new Response('[]', { headers: { 'Content-Type': 'application/json' } }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('affiche l\'empty state quand aucun abonnement', async () => {
    render(
      <MemoryRouter>
        <SubscriptionList />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(
        screen.getByRole('heading', { name: 'Aucun abonnement' }),
      ).toBeInTheDocument();
    });
  });

  it('affiche un abonnement actif', () => {
    useSubscriptionStore.setState({
      subscriptions: { 'sub-1': makeSub() },
      listLoaded: true,
    });
    render(
      <MemoryRouter>
        <SubscriptionList />
      </MemoryRouter>,
    );
    // « Actif » figure aussi sur le bouton de filtre : on vise la ligne.
    expect(within(screen.getAllByRole('row')[1]).getByText('Actif')).toBeInTheDocument();
    expect(screen.getByText('SUB-42')).toBeInTheDocument();
    // La colonne porte la valeur technique du champ provider, l'onglet
    // le nom commercial de la marque. Les deux doivent coexister : un
    // intégrateur filtre sur la première et se reconnaît dans le second.
    expect(within(screen.getAllByRole('row')[1]).getByText('payzen')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'PayZen' })).toBeInTheDocument();
  });

  it('affiche un abonnement annulé', () => {
    useSubscriptionStore.setState({
      subscriptions: { 'sub-2': makeSub({ id: 'sub-2', cancelled: true }) },
      listLoaded: true,
    });
    render(
      <MemoryRouter>
        <SubscriptionList />
      </MemoryRouter>,
    );
    expect(within(screen.getAllByRole('row')[1]).getByText('Annulé')).toBeInTheDocument();
  });

  // Le compteur répond en liste à « celui-là prélève-t-il ? » — la
  // question qu'on se pose quand une facturation récurrente ne tombe pas.
  it('affiche le compteur d\'échéances', () => {
    useSubscriptionStore.setState({
      subscriptions: {
        'sub-1': makeSub({ billingCount: 7 }),
        'sub-2': makeSub({ id: 'sub-2', orderId: 'SUB-VIDE', billingCount: 0 }),
      },
      listLoaded: true,
    });
    render(
      <MemoryRouter>
        <SubscriptionList />
      </MemoryRouter>,
    );
    expect(screen.getByText('7')).toBeInTheDocument();
    expect(screen.getByText('0')).toBeInTheDocument();
  });

  it('trie par createdAt décroissant', () => {
    useSubscriptionStore.setState({
      subscriptions: {
        old: makeSub({ id: 'old', orderId: 'OLD', createdAt: '2026-08-01T00:00:00Z' }),
        recent: makeSub({ id: 'recent', orderId: 'RECENT', createdAt: '2026-08-02T00:00:00Z' }),
      },
      listLoaded: true,
    });
    render(
      <MemoryRouter>
        <SubscriptionList />
      </MemoryRouter>,
    );
    const rows = screen.getAllByRole('row');
    // rows[0] = header, rows[1] = première ligne data
    expect(rows[1]).toHaveTextContent('RECENT');
    expect(rows[2]).toHaveTextContent('OLD');
  });
});
