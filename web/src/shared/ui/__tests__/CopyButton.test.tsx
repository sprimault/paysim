// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { CopyButton } from '@/shared/ui/CopyButton';
import { toast } from '@/shared/ui/toastStore';

const writeText = vi.fn().mockResolvedValue(undefined);

describe('CopyButton', () => {
  beforeEach(() => {
    writeText.mockClear();
    writeText.mockResolvedValue(undefined);
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

  // Une icône seule ne dit pas ce qui part au presse-papier : l'appelant
  // doit pouvoir le nommer.
  it('accepte un intitulé d\'action propre à l\'appelant', () => {
    render(<CopyButton value="curl ..." tip="Copier la commande curl" />);
    expect(screen.getByRole('button', { name: 'Copier la commande curl' })).toBeInTheDocument();
  });

  // Le catch etait vide depuis l'origine : on croyait avoir copie, et
  // on collait autre chose.
  it('signale une copie impossible au lieu de se taire', async () => {
    writeText.mockRejectedValue(new Error('NotAllowedError'));
    document.execCommand = vi.fn(() => false);
    const erreur = vi.spyOn(toast, 'error');
    render(<CopyButton value="x" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copier' }));
    await waitFor(() =>
      expect(erreur).toHaveBeenCalledWith('Copie impossible depuis ce navigateur'),
    );
    expect(screen.queryByRole('button', { name: 'Copié' })).not.toBeInTheDocument();
  });

  // C'est l'action qui varie, pas sa confirmation.
  it('confirme toujours par « Copié »', async () => {
    render(<CopyButton value="x" tip="Copier la commande curl" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copier la commande curl' }));
    expect(await screen.findByRole('button', { name: 'Copié' })).toBeInTheDocument();
  });
});
