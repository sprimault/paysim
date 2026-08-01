// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PaymentTimeline } from '@/features/payment-detail/ui/PaymentTimeline';
import type { EventEntry } from '@/shared/model';

const events: EventEntry[] = [
  { at: '2026-08-01T10:00:00Z', kind: 'created', amount: 4990 },
  { at: '2026-08-01T10:01:00Z', kind: 'authorized', amount: 4990 },
  { at: '2026-08-01T10:02:00Z', kind: 'captured', amount: 4990 },
];

describe('PaymentTimeline', () => {
  it('rend un item par événement avec son libellé', () => {
    render(<PaymentTimeline events={events} />);
    expect(screen.getByText('Créé')).toBeInTheDocument();
    expect(screen.getByText('Autorisé')).toBeInTheDocument();
    expect(screen.getByText('Capturé')).toBeInTheDocument();
  });

  it('affiche le montant formaté quand présent', () => {
    render(<PaymentTimeline events={events} />);
    expect(screen.getAllByText('49,90').length).toBe(3);
  });

  it('affiche la note quand présente', () => {
    render(
      <PaymentTimeline
        events={[
          { at: '2026-08-01T10:00:00Z', kind: 'declined', note: 'Fonds insuffisants' },
        ]}
      />,
    );
    expect(screen.getByText('Fonds insuffisants')).toBeInTheDocument();
  });
});
