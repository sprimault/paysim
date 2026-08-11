// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ListFilters, type FilterState } from '@/shared/ui/ListFilters';

const ETATS: FilterState[] = [
  { value: 'captured', labelKey: 'payment.state.captured' },
  { value: 'declined', labelKey: 'payment.state.declined' },
];

function renderFilters(props: Partial<Parameters<typeof ListFilters>[0]> = {}) {
  const onQueryChange = vi.fn();
  const onSelectedChange = vi.fn();
  render(
    <ListFilters
      query=""
      onQueryChange={onQueryChange}
      placeholderKey="common.filters.searchPayments"
      states={ETATS}
      selected={[]}
      onSelectedChange={onSelectedChange}
      shown={45}
      total={45}
      {...props}
    />,
  );
  return { onQueryChange, onSelectedChange };
}

describe('<ListFilters />', () => {
  it('rapporte la saisie', () => {
    const { onQueryChange } = renderFilters();
    fireEvent.change(screen.getByLabelText('Commande, UUID, alias…'), {
      target: { value: 'CMD-10' },
    });
    expect(onQueryChange).toHaveBeenCalledWith('CMD-10');
  });

  // Multi-selection : « refuses + expires » est une question courante.
  it('ajoute puis retire un etat sans toucher aux autres', () => {
    const { onSelectedChange } = renderFilters({ selected: ['captured'] });
    fireEvent.click(screen.getByText('Refusé'));
    expect(onSelectedChange).toHaveBeenCalledWith(['captured', 'declined']);

    fireEvent.click(screen.getByText('Payé'));
    expect(onSelectedChange).toHaveBeenLastCalledWith([]);
  });

  it('marque l\'etat retenu pour les lecteurs d\'ecran', () => {
    renderFilters({ selected: ['declined'] });
    expect(screen.getByText('Refusé')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByText('Payé')).toHaveAttribute('aria-pressed', 'false');
  });

  // Sans compteur, une liste filtree ressemble a une liste vide — et
  // l'on cherche une panne la ou il n'y a qu'un filtre oublie.
  it('annonce le nombre affiche des qu\'un filtre restreint', () => {
    renderFilters({ query: 'CMD', shown: 3, total: 45 });
    expect(screen.getByText('3 sur 45')).toBeInTheDocument();
  });

  it('sans filtre, ni compteur ni bouton d\'effacement', () => {
    renderFilters();
    expect(screen.queryByText('45 sur 45')).not.toBeInTheDocument();
    expect(screen.queryByText('Effacer')).not.toBeInTheDocument();
  });

  it('effacer remet la recherche et les etats a zero', () => {
    const { onQueryChange, onSelectedChange } = renderFilters({
      query: 'CMD',
      selected: ['declined'],
      shown: 2,
    });
    fireEvent.click(screen.getByText('Effacer'));
    expect(onQueryChange).toHaveBeenCalledWith('');
    expect(onSelectedChange).toHaveBeenCalledWith([]);
  });

  // Une liste sans etats a filtrer ne doit pas afficher une rangee vide.
  it('sans etats declares, aucun bouton d\'etat', () => {
    renderFilters({ states: [] });
    expect(screen.queryByText('Payé')).not.toBeInTheDocument();
  });
});
