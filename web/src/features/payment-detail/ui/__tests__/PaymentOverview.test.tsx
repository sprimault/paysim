// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PaymentOverview } from '@/features/payment-detail/ui/PaymentOverview';
import type { PaymentInStore } from '@/entities/payment/model/paymentStore';

const paiement: PaymentInStore = {
  uuid: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
  provider: 'systempay',
  orderId: 'CMD-SP-01',
  amount: 3990,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-01T12:00:00Z',
  updatedAt: '2026-08-01T12:05:00Z',
  webhookCount: 1,
  webhookReplayCount: 0,
};

function afficher(p: PaymentInStore = paiement) {
  return render(
    <MemoryRouter>
      <PaymentOverview payment={p} />
    </MemoryRouter>,
  );
}

describe('PaymentOverview', () => {
  // La fiche abonnement et la fiche moyen de paiement montrent leur
  // marque ; celle du paiement était la seule à la taire, alors que
  // c'est là qu'on arrive depuis une liste filtrée.
  it('montre la marque du paiement', () => {
    afficher();
    expect(screen.getByText('Provider')).toBeInTheDocument();
    expect(screen.getByText('systempay')).toBeInTheDocument();
  });

  it('rend la marque telle qu’elle est stockée, pas le libellé des onglets', () => {
    afficher({ ...paiement, provider: 'lyra' });
    expect(screen.getByText('lyra')).toBeInTheDocument();
    expect(screen.queryByText('Lyra Collect')).not.toBeInTheDocument();
  });
});
