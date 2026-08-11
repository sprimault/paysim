// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useMemo, useState } from 'react';
import { matchesSearch } from '@/shared/lib/strings';

interface Options<T> {
  /** Valeur de l'onglet de provider ; vide = tous. */
  provider: string;
  /** Provider de la ligne, comparé à l'onglet. */
  providerOf: (row: T) => string;
  /** Champs offerts à la recherche textuelle. */
  searchFields: (row: T) => (string | undefined)[];
  /** État de la ligne, comparé aux états retenus. */
  stateOf: (row: T) => string;
}

interface Resultat<T> {
  query: string;
  setQuery: (q: string) => void;
  etats: string[];
  setEtats: (s: string[]) => void;
  /** Lignes satisfaisant les trois filtres. */
  filtered: T[];
  /**
   * Total de référence pour « 3 sur 45 ». L'onglet de provider en fait
   * partie : il relève du contexte qu'on a choisi, pas du filtre qu'on
   * signale — sinon le compteur annoncerait un écart permanent dès
   * qu'un onglet est sélectionné.
   */
  total: number;
}

/**
 * useListFilters — recherche, états et provider pour une liste.
 *
 * Les trois listes de l'interface posent la même question : que reste-t-il
 * quand on croise l'onglet courant, une recherche libre et une sélection
 * d'états ? Seuls changent les champs cherchés et la façon de lire l'état,
 * que l'appelant fournit.
 *
 * Les trois filtres se cumulent, aucun n'est prioritaire. Le filtrage a
 * lieu côté client : la liste est déjà chargée en entier, et un aller-
 * retour serveur pour restreindre ce qu'on a sous la main coûterait une
 * latence sans rien apporter.
 *
 * L'état n'est pas persisté, délibérément : un filtre retenu d'un écran
 * à l'autre fait prendre une liste filtrée pour une liste vide, et l'on
 * cherche une panne là où il n'y a qu'un filtre oublié.
 */
export function useListFilters<T>(rows: T[], opts: Options<T>): Resultat<T> {
  const [query, setQuery] = useState('');
  const [etats, setEtats] = useState<string[]>([]);
  const { provider, providerOf, searchFields, stateOf } = opts;

  const parProvider = useMemo(
    () => rows.filter((r) => !provider || providerOf(r) === provider),
    // providerOf et les autres extracteurs sont redéfinis à chaque rendu
    // de l'appelant ; les mettre en dépendance relancerait le calcul à
    // chaque frappe pour rien.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [rows, provider],
  );

  const filtered = useMemo(
    () =>
      parProvider
        .filter((r) => matchesSearch(query, ...searchFields(r)))
        .filter((r) => etats.length === 0 || etats.includes(stateOf(r))),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [parProvider, query, etats],
  );

  return { query, setQuery, etats, setEtats, filtered, total: parProvider.length };
}
