// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PaymentRow } from '@/features/payment-list/ui/PaymentRow';
import type { PaymentSummary } from '@/shared/model';

const p: PaymentSummary = {
  uuid: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
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

  it('rend un bouton de copie pour l\'uuid', () => {
    renderRow();
    const copyButtons = screen.getAllByRole('button', { name: /copier/i });
    expect(copyButtons.length).toBeGreaterThan(0);
  });
});
