// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PaymentRow } from '@/features/payment-list/ui/PaymentRow';
import type { PaymentSummary } from '@/shared/model';

const p: PaymentSummary = {
  uuid: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
  provider: 'payzen',
  orderId: 'CMD-42',
  amount: 1299,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-01T12:00:00Z',
  updatedAt: '2026-08-01T12:05:00Z',
};

function renderRow(payment: PaymentSummary = p) {
  return render(
    <MemoryRouter>
      <table>
        <tbody>
          <PaymentRow payment={payment} />
        </tbody>
      </table>
    </MemoryRouter>,
  );
}

describe('PaymentRow', () => {
  it('rend le libellé d\'état, le montant, la devise et l\'orderId', () => {
    renderRow();
    expect(screen.getByText('Payé')).toBeInTheDocument();
    expect(screen.getByText('12,99')).toBeInTheDocument();
    expect(screen.getByText('EUR')).toBeInTheDocument();
    expect(screen.getByText('CMD-42')).toBeInTheDocument();
  });

  it('link vers la page détail avec le bon uuid', () => {
    renderRow();
    const link = screen.getByRole('link', { name: /ouvrir/i });
    expect(link).toHaveAttribute('href', `/payments/${p.uuid}`);
  });

  // Le motif decide de la suite chez le marchand : un 51 se retente, un
  // 43 impose de reclamer une autre carte. Il etait livre par l'API et
  // affiche nulle part — il fallait ouvrir la charge utile et lire du
  // JSON pour le trouver.
  it('affiche le code du motif de refus, libelle en infobulle', () => {
    renderRow({ ...p, state: 'declined', declineCode: '51', declineMessage: 'provision insuffisante' });
    const badge = screen.getByText('51');
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute('title', 'provision insuffisante');
  });

  // Un abandon ou une expiration n'ont pas de code bancaire : un badge
  // vide vaudrait moins que pas de badge du tout.
  it('sans motif, aucun badge de refus', () => {
    renderRow({ ...p, state: 'declined' });
    expect(screen.queryByTitle(/provision/)).not.toBeInTheDocument();
  });

  it('rend un bouton de copie pour l\'uuid', () => {
    renderRow();
    const copyButtons = screen.getAllByRole('button', { name: /copier/i });
    expect(copyButtons.length).toBeGreaterThan(0);
  });
});
