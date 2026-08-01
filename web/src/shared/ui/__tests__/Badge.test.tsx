// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Badge } from '@/shared/ui/Badge';

describe('Badge', () => {
  it('rend le texte enfant', () => {
    render(<Badge>Payé</Badge>);
    expect(screen.getByText('Payé')).toBeInTheDocument();
  });

  it('applique la classe de tone paid', () => {
    render(<Badge tone="paid">Payé</Badge>);
    const el = screen.getByText('Payé');
    expect(el.className).toMatch(/emerald/);
  });

  it('applique la classe de tone unpaid', () => {
    render(<Badge tone="unpaid">Refusé</Badge>);
    const el = screen.getByText('Refusé');
    expect(el.className).toMatch(/rose/);
  });

  it('rend l\'icône passée en prop', () => {
    render(
      <Badge tone="paid" icon={<span data-testid="icon">*</span>}>
        Payé
      </Badge>,
    );
    expect(screen.getByTestId('icon')).toBeInTheDocument();
  });
});
