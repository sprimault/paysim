// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { CopyButton } from '@/shared/ui/CopyButton';

const writeText = vi.fn().mockResolvedValue(undefined);

describe('CopyButton', () => {
  beforeEach(() => {
    writeText.mockClear();
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    });
  });

  it('écrit la valeur dans le presse-papier au clic', async () => {
    render(<CopyButton value="abc-123" />);
    fireEvent.click(screen.getByRole('button'));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('abc-123'));
  });

  it('rend le libellé optionnel', () => {
    render(<CopyButton value="x" label="Copier" />);
    expect(screen.getByText('Copier')).toBeInTheDocument();
  });

  it('bascule l\'aria-label après un clic réussi', async () => {
    render(<CopyButton value="x" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copier' }));
    expect(await screen.findByRole('button', { name: 'Copié' })).toBeInTheDocument();
  });
});
