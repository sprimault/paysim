// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FreshnessIndicator } from '@/widgets/header/FreshnessIndicator';

describe('<FreshnessIndicator />', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-08-11T10:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const instant = () => Date.now();

  // Tant que rien n'est arrivé, pas même l'ouverture du flux, il n'y a
  // aucun âge à afficher — un « il y a 0s » mentirait.
  it('ne rend rien avant le premier signe de vie', () => {
    const { container } = render(<FreshnessIndicator connected />);
    expect(container).toBeEmptyDOMElement();
  });

  it('annonce l\'instant juste après un événement', () => {
    render(<FreshnessIndicator connected lastEventAt={instant()} />);
    expect(screen.getByText("mis à jour à l'instant")).toBeInTheDocument();
  });

  // Le battement est ce qui distingue un écran vivant d'une
  // photographie : sans lui, l'âge affiché resterait celui du rendu.
  it('vieillit seconde après seconde', () => {
    render(<FreshnessIndicator connected lastEventAt={instant()} />);
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.getByText('mis à jour il y a 3s')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(120_000);
    });
    expect(screen.getByText('mis à jour il y a 2min 3s')).toBeInTheDocument();
  });

  // Flux fermé : l'âge cesse d'être une information anodine, c'est la
  // mesure de ce qu'on ne voit plus.
  it('alerte quand le flux est fermé', () => {
    const { rerender } = render(<FreshnessIndicator connected lastEventAt={instant()} />);
    expect(screen.getByText("mis à jour à l'instant").className).toContain('text-zinc-400');
    rerender(<FreshnessIndicator connected={false} lastEventAt={instant()} />);
    expect(screen.getByText("mis à jour à l'instant").className).toContain('text-amber-600');
  });

  // L'horloge du poste peut avancer sur l'estampille reçue — resynchro
  // NTP, changement d'heure. L'infobulle est le seul endroit où un âge
  // négatif se verrait : le libellé, lui, retombe sur « à l'instant ».
  it('ne rend jamais d\'âge négatif', () => {
    render(<FreshnessIndicator connected={false} lastEventAt={instant() + 5000} />);
    fireEvent.mouseEnter(screen.getByText("mis à jour à l'instant"));
    expect(screen.getByRole('tooltip')).toHaveTextContent(
      'Flux fermé : plus rien reçu depuis 0s',
    );
  });
});
