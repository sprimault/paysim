// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Search, X } from 'lucide-react';
import { useT } from '@/shared/i18n/useT';
import type { MessageKey } from '@/shared/i18n/messages';

/** Un état proposé au filtre, avec son libellé traduit. */
export interface FilterState {
  /** Valeur comparée à celle de la ligne. */
  value: string;
  labelKey: MessageKey;
}

interface ListFiltersProps {
  query: string;
  onQueryChange: (q: string) => void;
  /** Décrit ce que la recherche parcourt — « commande, UUID, alias ». */
  placeholderKey: MessageKey;

  /** États proposés. Vide, la rangée d'états disparaît. */
  states?: FilterState[];
  selected?: string[];
  onSelectedChange?: (s: string[]) => void;

  /** Nombre de lignes affichées et total, pour signaler un filtre actif. */
  shown: number;
  total: number;
}

/**
 * ListFilters — recherche et filtre par état, au-dessus d'une table.
 *
 * Le filtrage est fait par l'écran appelant, pas ici : ce composant ne
 * connaît ni les lignes ni leur forme. Il rend les contrôles et rapporte
 * l'état ; chaque liste sait seuls quels champs elle expose à la
 * recherche.
 *
 * Les deux filtres se composent — « les refusés dont la commande
 * contient CMD-10 » — parce que c'est la question qu'on se pose en
 * débogage : rarement l'une sans l'autre.
 *
 * Le compteur affiche « 3 sur 45 » dès qu'un filtre restreint. Sans
 * cela, une liste filtrée ressemble à une liste vide, et l'on cherche
 * une panne là où il n'y a qu'un filtre oublié — c'est pour la même
 * raison que rien n'est retenu d'un écran à l'autre.
 */
export function ListFilters({
  query,
  onQueryChange,
  placeholderKey,
  states = [],
  selected = [],
  onSelectedChange,
  shown,
  total,
}: ListFiltersProps) {
  const t = useT();
  const filtreActif = query.trim() !== '' || selected.length > 0;

  function basculer(value: string) {
    if (!onSelectedChange) return;
    onSelectedChange(
      selected.includes(value) ? selected.filter((s) => s !== value) : [...selected, value],
    );
  }

  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <div className="relative">
        <Search
          size={14}
          className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400 dark:text-zinc-500"
        />
        <input
          type="search"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder={t(placeholderKey)}
          aria-label={t(placeholderKey)}
          className="h-8 w-64 rounded-md border border-zinc-200 bg-white pl-8 pr-2 text-sm text-zinc-900 placeholder:text-zinc-400 focus:border-brand-400 focus:outline-none dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-100 dark:placeholder:text-zinc-600"
        />
      </div>

      {states.map((s) => {
        const actif = selected.includes(s.value);
        return (
          <button
            key={s.value}
            type="button"
            aria-pressed={actif}
            onClick={() => basculer(s.value)}
            className={
              'h-8 rounded-md border px-2.5 text-xs font-medium transition-colors ' +
              (actif
                ? 'border-brand-300 bg-brand-100 text-brand-800 dark:border-brand-700 dark:bg-brand-900/40 dark:text-brand-300'
                : 'border-zinc-200 bg-white text-zinc-600 hover:bg-zinc-50 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800')
            }
          >
            {t(s.labelKey)}
          </button>
        );
      })}

      {filtreActif && (
        <>
          <span className="text-xs tabular text-zinc-500 dark:text-zinc-400">
            {t('common.filters.count', { shown, total })}
          </span>
          <button
            type="button"
            onClick={() => {
              onQueryChange('');
              onSelectedChange?.([]);
            }}
            className="inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs text-zinc-500 hover:bg-zinc-100 hover:text-zinc-800 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
          >
            <X size={12} />
            {t('common.filters.clear')}
          </button>
        </>
      )}
    </div>
  );
}
