// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Tooltip } from '@/shared/ui/Tooltip';

/**
 * jsdom ne fait pas de mise en page : toutes les mesures valent zéro.
 * On les simule pour éprouver le placement, qui est justement ce que ce
 * composant apporte par rapport à un attribut `title`.
 */
function mesurer({
  ancre,
  boite = { w: 160, h: 32 },
}: {
  ancre: { top: number; bottom: number; left: number; width: number };
  boite?: { w: number; h: number };
}) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
    if (this.getAttribute('role') === 'tooltip') return new DOMRect();
    return {
      top: ancre.top,
      bottom: ancre.bottom,
      left: ancre.left,
      right: ancre.left + ancre.width,
      width: ancre.width,
      height: ancre.bottom - ancre.top,
      x: ancre.left,
      y: ancre.top,
      toJSON: () => ({}),
    } as DOMRect;
  });
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(boite.w);
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(boite.h);
}

function boite() {
  return screen.getByRole('tooltip');
}

/** Le chevron est le seul enfant pivoté de la boîte. */
function chevron() {
  const el = boite().querySelector('.rotate-45');
  if (!el) throw new Error('chevron absent');
  return el as HTMLElement;
}

describe('<Tooltip />', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('ne rend rien tant qu\'on ne survole pas', () => {
    render(
      <Tooltip label="provision insuffisante">
        <span>Refusé</span>
      </Tooltip>,
    );
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('apparaît au survol et disparaît en sortant', () => {
    render(
      <Tooltip label="provision insuffisante">
        <span>Refusé</span>
      </Tooltip>,
    );
    const zone = screen.getByLabelText('provision insuffisante');
    fireEvent.mouseEnter(zone);
    expect(boite()).toHaveTextContent('provision insuffisante');
    fireEvent.mouseLeave(zone);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  // Le clavier aussi : le survol n'existe pas pour qui tabule.
  it('apparaît au focus', () => {
    render(
      <Tooltip label="carte volee">
        <span>Refusé</span>
      </Tooltip>,
    );
    fireEvent.focus(screen.getByLabelText('carte volee'));
    expect(boite()).toHaveTextContent('carte volee');
  });

  // Sans libellé, ni curseur ni zone survolable : rien n'annonce une
  // lecture qui n'existe pas.
  it('sans libellé, passe-plat pur', () => {
    const { container } = render(
      <Tooltip>
        <span>Refusé</span>
      </Tooltip>,
    );
    expect(screen.getByText('Refusé')).toBeInTheDocument();
    expect(container.querySelector('.cursor-pointer')).toBeNull();
    fireEvent.mouseEnter(screen.getByText('Refusé'));
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('se place au-dessus quand la place le permet, chevron vers le bas', () => {
    mesurer({ ancre: { top: 400, bottom: 420, left: 300, width: 80 } });
    render(
      <Tooltip label="motif">
        <span>Refusé</span>
      </Tooltip>,
    );
    fireEvent.mouseEnter(screen.getByLabelText('motif'));

    // 400 - 32 (hauteur) - 8 (marge) = 360.
    expect(boite()).toHaveStyle({ top: '360px' });
    // Chevron sous la boîte : il désigne l'ancre, qui est en dessous.
    expect(chevron().style.bottom).toBe('-4px');
    expect(chevron().style.top).toBe('');
  });

  // Contre le haut de la fenêtre, la boîte bascule plutôt que de sortir
  // de l'écran — et le chevron suit.
  it('bascule en dessous quand le haut manque, chevron vers le haut', () => {
    mesurer({ ancre: { top: 10, bottom: 30, left: 300, width: 80 } });
    render(
      <Tooltip label="motif">
        <span>Refusé</span>
      </Tooltip>,
    );
    fireEvent.mouseEnter(screen.getByLabelText('motif'));

    // 30 (bas de l'ancre) + 8 (marge) = 38.
    expect(boite()).toHaveStyle({ top: '38px' });
    expect(chevron().style.top).toBe('-4px');
    expect(chevron().style.bottom).toBe('');
  });

  // Une boîte centrée sur une ancre collée au bord sortirait de l'écran.
  it('reste dans la fenêtre près du bord gauche', () => {
    mesurer({ ancre: { top: 400, bottom: 420, left: 0, width: 40 } });
    render(
      <Tooltip label="motif">
        <span>Refusé</span>
      </Tooltip>,
    );
    fireEvent.mouseEnter(screen.getByLabelText('motif'));

    // Centrée, elle serait à 20 - 80 = -60. Ramenée à la marge de bord.
    expect(boite()).toHaveStyle({ left: '8px' });
    // Le chevron vise toujours l'ancre malgré le recalage.
    expect(chevron().style.left).toBe('8px');
  });
});
