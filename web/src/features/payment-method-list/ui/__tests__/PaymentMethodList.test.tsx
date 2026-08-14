// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PaymentMethodList } from '@/features/payment-method-list/ui/PaymentMethodList';
import { usePaymentMethodStore } from '@/entities/payment-method/model/paymentMethodStore';
import { useClockStore } from '@/shared/model/clockStore';
import type { PaymentMethodOutput } from '@/shared/model';

function makeMethod(overrides: Partial<PaymentMethodOutput> = {}): PaymentMethodOutput {
  return {
    token: 'pmt-1',
    provider: 'payzen',
    panMasked: '411111XXXXXX1111',
    brand: 'VISA',
    expiryMonth: 12,
    expiryYear: 2028,
    revoked: false,
    usable: true,
    createdAt: '2026-08-02T10:00:00Z',
    ...overrides,
  };
}

describe('<PaymentMethodList />', () => {
  beforeEach(() => {
    usePaymentMethodStore.setState({ methods: {}, listLoaded: true });
    // Le décalage d'horloge est global : sans remise à zéro, un test
    // qui avance le temps le laisse à ceux qui suivent.
    useClockStore.setState({ decalageMs: 0, charge: true });
    vi.spyOn(global, 'fetch').mockImplementation(async () => new Response('[]', { headers: { 'Content-Type': 'application/json' } }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('affiche l\'empty state quand aucun moyen', async () => {
    render(
      <MemoryRouter>
        <PaymentMethodList />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText(/Aucun moyen de paiement/)).toBeInTheDocument();
    });
  });

  it('affiche un moyen actif avec PAN masqué', () => {
    usePaymentMethodStore.setState({
      methods: { 'pmt-1': makeMethod() },
      listLoaded: true,
    });
    render(
      <MemoryRouter>
        <PaymentMethodList />
      </MemoryRouter>,
    );
    // « Actif » figure aussi sur le bouton de filtre : on vise la ligne.
    expect(within(screen.getAllByRole('row')[1]).getByText('Actif')).toBeInTheDocument();
    expect(screen.getByText('VISA')).toBeInTheDocument();
    expect(screen.getByText('411111XXXXXX1111')).toBeInTheDocument();
    expect(screen.getByText('12/2028')).toBeInTheDocument();
  });

  it('affiche un moyen révoqué', () => {
    usePaymentMethodStore.setState({
      methods: { 'pmt-2': makeMethod({ token: 'pmt-2', revoked: true }) },
      listLoaded: true,
    });
    render(
      <MemoryRouter>
        <PaymentMethodList />
      </MemoryRouter>,
    );
    expect(within(screen.getAllByRole('row')[1]).getByText('Révoqué')).toBeInTheDocument();
  });

  // Le badge se calculait sur l'horloge du poste : sur une instance
  // avancée d'un mois, une carte que le serveur refusait déjà
  // s'affichait « Actif ». L'écran contredisait l'API sans rien
  // signaler — le mensonge silencieux que ce simulateur existe pour
  // éviter.
  it('suit l’horloge du simulateur pour l’expiration', () => {
    const mois = new Date().getMonth() + 1;
    const annee = new Date().getFullYear();
    usePaymentMethodStore.setState({
      methods: { 'pmt-3': makeMethod({ token: 'pmt-3', expiryMonth: mois, expiryYear: annee }) },
      listLoaded: true,
    });
    useClockStore.setState({ decalageMs: 0, charge: true });
    const { rerender } = render(
      <MemoryRouter>
        <PaymentMethodList />
      </MemoryRouter>,
    );
    expect(within(screen.getAllByRole('row')[1]).getByText('Actif')).toBeInTheDocument();

    // Soixante jours : franchit la fin du mois d'expiration quel que
    // soit le jour où le test s'exécute.
    useClockStore.setState({ decalageMs: 60 * 24 * 3600 * 1000 });
    rerender(
      <MemoryRouter>
        <PaymentMethodList />
      </MemoryRouter>,
    );
    expect(within(screen.getAllByRole('row')[1]).getByText('Expiré')).toBeInTheDocument();
  });

  it('trie par createdAt décroissant', () => {
    usePaymentMethodStore.setState({
      methods: {
        old: makeMethod({ token: 'old', panMasked: '400000XXXXXX0002', createdAt: '2026-08-01T00:00:00Z' }),
        recent: makeMethod({ token: 'recent', panMasked: '510510XXXXXX5100', createdAt: '2026-08-02T00:00:00Z' }),
      },
      listLoaded: true,
    });
    render(
      <MemoryRouter>
        <PaymentMethodList />
      </MemoryRouter>,
    );
    const rows = screen.getAllByRole('row');
    // rows[0] = header
    expect(rows[1]).toHaveTextContent('510510XXXXXX5100');
    expect(rows[2]).toHaveTextContent('400000XXXXXX0002');
  });
});
