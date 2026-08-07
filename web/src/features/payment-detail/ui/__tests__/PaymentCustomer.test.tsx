// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PaymentCustomer } from '@/features/payment-detail/ui/PaymentCustomer';
import type { Customer } from '@/shared/model';

describe('PaymentCustomer', () => {
  it('annonce l’absence de contexte plutôt que des rubriques vides', () => {
    render(<PaymentCustomer />);
    expect(screen.getByText(/Aucun contexte client/)).toBeInTheDocument();
    expect(screen.queryByText('Livraison')).not.toBeInTheDocument();
  });

  it('affiche les trois blocs quand ils sont renseignés', () => {
    const customer: Customer = {
      email: 'alice@example.com',
      reference: 'demo-org',
      billingDetails: { firstName: 'Alice', lastName: 'MARTIN', zipCode: '75002', city: 'Paris' },
      shippingDetails: {
        category: 'COMPANY',
        legalName: 'ACME SARL',
        streetNumber: '12',
        address: 'avenue des Champs',
        deliveryCompanyName: 'TRANSPORTEUR X',
        shippingMethod: 'RELAY_POINT',
      },
      extraDetails: { ipAddress: '203.0.113.7', fingerPrintId: 'fp-abc' },
    };
    render(<PaymentCustomer customer={customer} metadata={{ plan: 'pro' }} />);

    expect(screen.getByText('demo-org')).toBeInTheDocument();
    expect(screen.getByText('Alice MARTIN')).toBeInTheDocument();
    expect(screen.getByText('ACME SARL')).toBeInTheDocument();
    expect(screen.getByText('RELAY_POINT')).toBeInTheDocument();
    expect(screen.getByText('203.0.113.7')).toBeInTheDocument();
    expect(screen.getByText('pro')).toBeInTheDocument();

    // L'adresse de livraison est recollée : PayZen la découpe pour ses
    // règles antifraude, on la lit comme une adresse.
    expect(screen.getByText('12, avenue des Champs')).toBeInTheDocument();
  });

  it('masque une section dont aucun champ n’est renseigné', () => {
    const customer: Customer = { email: 'bob@example.com' };
    render(<PaymentCustomer customer={customer} />);

    expect(screen.getByText('Identité')).toBeInTheDocument();
    expect(screen.queryByText('Livraison')).not.toBeInTheDocument();
    expect(screen.queryByText('Contexte navigateur')).not.toBeInTheDocument();
  });
});
