// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useListFilters } from '@/shared/hooks/useListFilters';

interface Ligne {
  provider: string;
  orderId: string;
  uuid: string;
  state: string;
}

const LIGNES: Ligne[] = [
  { provider: 'payzen', orderId: 'CMD-1042', uuid: 'aaa-111', state: 'captured' },
  { provider: 'payzen', orderId: 'CMD-1043', uuid: 'bbb-222', state: 'declined' },
  { provider: 'payzen', orderId: 'SUB-77', uuid: 'ccc-333', state: 'declined' },
  { provider: 'stripe', orderId: 'CMD-9', uuid: 'ddd-444', state: 'captured' },
];

function monter(provider = '') {
  return renderHook(() =>
    useListFilters(LIGNES, {
      provider,
      providerOf: (l) => l.provider,
      searchFields: (l) => [l.orderId, l.uuid],
      stateOf: (l) => l.state,
    }),
  );
}

describe('useListFilters', () => {
  it('sans filtre, rend tout', () => {
    const { result } = monter();
    expect(result.current.filtered).toHaveLength(4);
    expect(result.current.total).toBe(4);
  });

  it('cherche sur plusieurs champs, insensible a la casse', () => {
    const { result } = monter();
    act(() => result.current.setQuery('cmd-104'));
    expect(result.current.filtered.map((l) => l.orderId)).toEqual(['CMD-1042', 'CMD-1043']);

    act(() => result.current.setQuery('ccc'));
    expect(result.current.filtered.map((l) => l.orderId)).toEqual(['SUB-77']);
  });

  it('filtre par etats, en multi-selection', () => {
    const { result } = monter();
    act(() => result.current.setEtats(['declined']));
    expect(result.current.filtered).toHaveLength(2);

    act(() => result.current.setEtats(['declined', 'captured']));
    expect(result.current.filtered).toHaveLength(4);
  });

  // Les trois filtres se cumulent — c'est la question qu'on se pose en
  // deboguant : « les refuses dont la commande contient CMD ».
  it('cumule recherche et etats', () => {
    const { result } = monter();
    act(() => {
      result.current.setQuery('CMD');
      result.current.setEtats(['declined']);
    });
    expect(result.current.filtered.map((l) => l.orderId)).toEqual(['CMD-1043']);
  });

  // Le total sert de reference au « 3 sur 45 » : l'onglet de provider
  // releve du contexte choisi, pas du filtre qu'on signale.
  it('le total suit le provider, pas la recherche', () => {
    const { result } = monter('payzen');
    expect(result.current.total).toBe(3);
    act(() => result.current.setQuery('CMD-1042'));
    expect(result.current.filtered).toHaveLength(1);
    expect(result.current.total).toBe(3);
  });

  it('une recherche sans resultat rend une liste vide, pas tout', () => {
    const { result } = monter();
    act(() => result.current.setQuery('introuvable'));
    expect(result.current.filtered).toHaveLength(0);
  });
});
