// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';

describe('ConfirmDialog', () => {
  it('ne rend rien quand open=false', () => {
    render(
      <ConfirmDialog
        open={false}
        title="X"
        description="Y"
        onConfirm={() => undefined}
        onCancel={() => undefined}
      />,
    );
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('rend titre + description quand open=true', () => {
    render(
      <ConfirmDialog
        open
        title="Titre"
        description="Description"
        onConfirm={() => undefined}
        onCancel={() => undefined}
      />,
    );
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Titre')).toBeInTheDocument();
    expect(screen.getByText('Description')).toBeInTheDocument();
  });

  it('appelle onConfirm au clic sur le bouton de confirmation', async () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open
        title="X"
        description="Y"
        confirmLabel="Ok"
        onConfirm={onConfirm}
        onCancel={() => undefined}
      />,
    );
    // Le bouton s'arme après un court délai, cf. le test suivant.
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Ok' })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Ok' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  // Contrepartie de l'ancrage au déclencheur : la boîte s'ouvre sous le
  // curseur qui vient de cliquer, donc le bouton de validation se
  // retrouve à quelques pixels du point de clic. Sans ce délai, un
  // double-clic un peu vif viderait la base avant que l'œil ait lu la
  // question.
  it('n\'arme le bouton de confirmation qu\'après un court délai', async () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open
        title="X"
        description="Y"
        confirmLabel="Ok"
        onConfirm={onConfirm}
        onCancel={() => undefined}
      />,
    );

    const bouton = screen.getByRole('button', { name: 'Ok' });
    expect(bouton).toBeDisabled();
    fireEvent.click(bouton);
    expect(onConfirm).not.toHaveBeenCalled();

    await waitFor(() => expect(bouton).toBeEnabled());
    fireEvent.click(bouton);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  // Annuler reste accessible immédiatement : le délai protège de
  // l'action destructive, pas du renoncement.
  it('laisse annuler sans délai', () => {
    const onCancel = vi.fn();
    render(
      <ConfirmDialog
        open
        title="X"
        description="Y"
        cancelLabel="Non"
        onConfirm={() => undefined}
        onCancel={onCancel}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Non' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('appelle onCancel au clic sur Annuler', () => {
    const onCancel = vi.fn();
    render(
      <ConfirmDialog
        open
        title="X"
        description="Y"
        cancelLabel="Annuler"
        onConfirm={() => undefined}
        onCancel={onCancel}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Annuler' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('appelle onCancel sur Escape', () => {
    const onCancel = vi.fn();
    render(
      <ConfirmDialog
        open
        title="X"
        description="Y"
        onConfirm={() => undefined}
        onCancel={onCancel}
      />,
    );
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('bloque les interactions quand loading=true', () => {
    const onCancel = vi.fn();
    render(
      <ConfirmDialog
        open
        loading
        title="X"
        description="Y"
        onConfirm={() => undefined}
        onCancel={onCancel}
      />,
    );
    // Escape ignoré pendant le loading.
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onCancel).not.toHaveBeenCalled();
  });
});
