// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ClockControl } from '@/widgets/header/ClockControl';
import { useClockStore } from '@/shared/model/clockStore';
import * as client from '@/shared/api/client';

describe('ClockControl', () => {
  beforeEach(() => {
    useClockStore.setState({ decalageMs: 0, charge: false });
    vi.spyOn(client, 'apiGetJson').mockResolvedValue({
      now: new Date().toISOString(),
      offset: '0s',
      offsetSeconds: 0,
    });
  });
  afterEach(() => vi.restoreAllMocks());

  // Au repos, l'en-tête ne doit pas s'encombrer : l'icône suffit.
  it('ne montre aucun bandeau à l’heure réelle', () => {
    render(<ClockControl />);
    expect(screen.queryByTestId('clock-shifted')).toBeNull();
  });

  // C'est le rôle principal du composant : sans ce bandeau, des dates
  // futures s'afficheraient sans explication.
  it('signale une instance décalée', () => {
    useClockStore.setState({ decalageMs: 96 * 3600 * 1000, charge: true });
    render(<ClockControl />);
    expect(screen.getByTestId('clock-shifted')).toBeInTheDocument();
  });

  it('avance d’un jour au clic', async () => {
    const post = vi.spyOn(client, 'apiPostJson').mockResolvedValue({
      now: new Date().toISOString(),
      offset: '24h0m0s',
      offsetSeconds: 86400,
    });
    const user = userEvent.setup();
    render(<ClockControl />);

    await user.click(screen.getByRole('button', { name: /avancer le temps|advance time/i }));
    await user.click(screen.getByRole('button', { name: /\+1 (jour|day)/i }));

    expect(post).toHaveBeenCalledWith('/clock/advance', { duration: '24h' });
  });

  // Le décalage ne bouge que sur action explicite : une seule lecture
  // au montage suffit, et c'est ce que le composant annonce.
  //
  // Il en émettait davantage : `t` changeait d'identité à chaque rendu,
  // l'effet la déclarait en dépendance, et la lecture qu'il déclenchait
  // écrivait dans le store — donc provoquait le rendu suivant. Chaque
  // gigue sur l'heure serveur relançait la boucle.
  it('ne lit l’horloge qu’une fois au montage', async () => {
    const get = vi.spyOn(client, 'apiGetJson').mockResolvedValue({
      now: new Date().toISOString(),
      offset: '0s',
      offsetSeconds: 0,
    });
    const { rerender } = render(<ClockControl />);

    // Laisser l'effet et sa promesse se résoudre, puis provoquer des
    // rendus supplémentaires : aucun ne doit relire l'horloge.
    await act(async () => {
      await Promise.resolve();
    });
    rerender(<ClockControl />);
    rerender(<ClockControl />);

    expect(get).toHaveBeenCalledTimes(1);
  });

  // Rien à réinitialiser quand rien n'a bougé : le bouton reste
  // inactif plutôt que d'émettre un appel sans effet.
  it('désactive la réinitialisation à l’heure réelle', async () => {
    const user = userEvent.setup();
    render(<ClockControl />);
    await user.click(screen.getByRole('button', { name: /avancer le temps|advance time/i }));
    expect(screen.getByRole('button', { name: /heure réelle|real time/i })).toBeDisabled();
  });
});
