// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Card } from '../Card';

describe('Card', () => {
  it('rend les enfants', () => {
    render(<Card>contenu</Card>);
    expect(screen.getByText('contenu')).toBeInTheDocument();
  });

  it('applique le padding uniquement quand padded=true', () => {
    const { rerender, container } = render(<Card>x</Card>);
    expect(container.firstChild).not.toHaveClass('p-4');
    rerender(<Card padded>x</Card>);
    expect(container.firstChild).toHaveClass('p-4');
  });
});
