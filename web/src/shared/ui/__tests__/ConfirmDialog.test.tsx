// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
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

  it('appelle onConfirm au clic sur le bouton de confirmation', () => {
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
    fireEvent.click(screen.getByRole('button', { name: 'Ok' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
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
