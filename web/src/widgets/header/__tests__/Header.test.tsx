// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { Header } from '@/widgets/header/Header';

function renderHeader(props: Parameters<typeof Header>[0] = {}) {
  return render(
    <MemoryRouter>
      <Header {...props} />
    </MemoryRouter>,
  );
}

describe('Header', () => {
  it('rend le logo Paysim en lien vers la racine', () => {
    renderHeader();
    const link = screen.getByRole('link', { name: /paysim/i });
    expect(link).toHaveAttribute('href', '/');
  });

  it('affiche Connecté par défaut', () => {
    renderHeader();
    expect(screen.getByText('Connecté')).toBeInTheDocument();
  });

  it('affiche Déconnecté quand connected=false', () => {
    renderHeader({ connected: false });
    expect(screen.getByText('Déconnecté')).toBeInTheDocument();
  });
});
