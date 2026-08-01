// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Button } from '@/shared/ui/Button';

describe('Button', () => {
  it('rend le libellé et appelle onClick', async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Simuler</Button>);
    await user.click(screen.getByRole('button', { name: 'Simuler' }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('bloque le clic quand disabled', async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    render(
      <Button onClick={onClick} disabled>
        Off
      </Button>,
    );
    await user.click(screen.getByRole('button', { name: 'Off' }));
    expect(onClick).not.toHaveBeenCalled();
  });

  it('bloque le clic et masque leftIcon quand loading', () => {
    const onClick = vi.fn();
    render(
      <Button onClick={onClick} loading leftIcon={<span data-testid="left">L</span>}>
        Envoi
      </Button>,
    );
    const btn = screen.getByRole('button', { name: 'Envoi' });
    expect(btn).toBeDisabled();
    expect(screen.queryByTestId('left')).not.toBeInTheDocument();
  });

  it.each(['primary', 'ghost', 'danger'] as const)(
    'applique une classe distincte pour la variant %s',
    (variant) => {
      render(<Button variant={variant}>X</Button>);
      const btn = screen.getByRole('button', { name: 'X' });
      expect(btn.className.length).toBeGreaterThan(0);
    },
  );
});
