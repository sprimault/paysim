// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CreditCard } from 'lucide-react';
import { EmptyState } from '@/shared/ui/EmptyState';

describe('EmptyState', () => {
  it('rend le titre et le hint', () => {
    render(<EmptyState icon={CreditCard} title="Aucun paiement" hint="Rien pour l'instant" />);
    expect(screen.getByText('Aucun paiement')).toBeInTheDocument();
    expect(screen.getByText("Rien pour l'instant")).toBeInTheDocument();
  });

  it('omet le hint quand absent', () => {
    render(<EmptyState icon={CreditCard} title="Rien" />);
    expect(screen.queryByText("Rien pour l'instant")).not.toBeInTheDocument();
  });

  it('rend le bouton d\'action passé en prop', () => {
    render(
      <EmptyState
        icon={CreditCard}
        title="Rien"
        action={<button type="button">Créer</button>}
      />,
    );
    expect(screen.getByRole('button', { name: 'Créer' })).toBeInTheDocument();
  });
});
