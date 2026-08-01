// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PaymentList } from '../PaymentList';
import { mockPayments } from '../../../../shared/lib/mocks';

describe('PaymentList', () => {
  it('rend le titre et le compteur de paiements mocks', () => {
    render(
      <MemoryRouter>
        <PaymentList />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { name: 'Paiements' })).toBeInTheDocument();
    expect(
      screen.getByText(`${mockPayments.length} paiements en mémoire`),
    ).toBeInTheDocument();
  });

  it('rend une ligne par paiement mock', () => {
    render(
      <MemoryRouter>
        <PaymentList />
      </MemoryRouter>,
    );
    for (const p of mockPayments) {
      expect(screen.getByText(p.orderId)).toBeInTheDocument();
    }
  });
});
