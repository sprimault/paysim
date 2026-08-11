// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { ChevronDown, ChevronUp, ChevronsUpDown } from 'lucide-react';
import { Skeleton } from '@/shared/ui/Skeleton';
import { useT } from '@/shared/i18n/useT';

/**
 * Colonne déclarative — un header et une fonction cellule qui reçoit
 * la row. Le rendu est libre (badges, liens, formatage), le composant
 * DataTable ne fait qu'orchestrer la structure `<table>` et les
 * styles cohérents avec le reste de l'UI Paysim.
 */
export interface Column<T> {
  header: ReactNode;
  cell: (row: T) => ReactNode;
  /** Alignement du header et de la cellule — défaut left. */
  align?: 'left' | 'right';
  /** Classes Tailwind additionnelles appliquées au `<td>`. */
  className?: string;
  /** Rend la cellule visible aux screen-reader mais pas visuellement. */
  srOnly?: boolean;
  /**
   * Valeur comparable extraite de la ligne. Sa présence rend la colonne
   * triable ; son absence laisse l'en-tête inerte. On trie sur cette
   * valeur et non sur le rendu, parce qu'une cellule contient un badge,
   * un lien ou un montant déjà formaté — comparer « 1 234,00 » à
   * « 987,00 » comme des chaînes donnerait un ordre faux.
   */
  sortValue?: (row: T) => string | number;
}

/** Sens de tri courant. `null` = ordre fourni par l'appelant. */
type SortState = { column: number; dir: 'asc' | 'desc' } | null;

export interface DataTableProps<T> {
  /** Colonnes déclaratives : en-tête, rendu de cellule, alignement. */
  columns: Column<T>[];
  /** Lignes à rendre, dans l'ordre naturel voulu par l'appelant. */
  rows: T[];
  /** Extracteur d'identifiant unique pour la clé React. */
  rowKey: (row: T) => string;
  /** Contenu rendu quand rows est vide et loading est false. */
  emptyState?: ReactNode;
  /** Affiche un Skeleton tant que loading + rows vide. */
  loading?: boolean;
  /**
   * Nombre de lignes par page à l'ouverture. Omis, la table rend tout
   * d'un bloc — c'est le comportement voulu sur les listes courtes, où
   * une barre de navigation pour six lignes est du bruit.
   */
  pageSize?: number;
  /**
   * Paliers proposés dans le sélecteur. La taille initiale y est
   * insérée si elle n'y figure pas, sans quoi le sélecteur s'ouvrirait
   * sur une valeur qu'il ne propose pas.
   */
  pageSizeOptions?: number[];
  /**
   * Contrôles propres à l'écran — recherche, filtres — rendus au-dessus
   * de la pagination.
   *
   * Ils passent par ici plutôt que d'être posés à côté de la table pour
   * qu'un seul bloc les rende collants avec elle : empilés dans le même
   * conteneur, ils se placent l'un sous l'autre sans qu'on ait à
   * connaître leur hauteur. `DataTable` ne les interprète jamais.
   */
  toolbar?: ReactNode;
}

/**
 * Table dense et sobre — pas de cards, pas d'ombre. Aligné sur le
 * pattern PaymentList existant, factorisé pour subscriptions et
 * payment-methods (règle de trois anticipée : 3 usages prévus).
 * L'header, la sélection Tailwind, les hover-rows et les bordures
 * dark: sont figés ici pour rester cohérents à travers l'app.
 *
 * Le tri et la pagination vivent ici plutôt que dans chaque écran, et
 * sans bibliothèque : le bundle finit dans le binaire Go, et react-table
 * pèse plus que les quarante lignes qu'il remplacerait. Les deux sont
 * opt-in — une colonne sans `sortValue` n'est pas triable, une table
 * sans `pageSize` n'est pas paginée.
 *
 * L'état de tri est local au composant. Conséquence assumée : il se perd
 * quand l'écran se démonte. Le persister supposerait de choisir où — URL,
 * store, session — un arbitrage que rien ne réclame aujourd'hui.
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  emptyState,
  loading,
  pageSize,
  pageSizeOptions = [10, 25, 50, 100],
  toolbar,
}: DataTableProps<T>) {
  const t = useT();
  const [sort, setSort] = useState<SortState>(null);
  const [page, setPage] = useState(0);
  const [taille, setTaille] = useState(pageSize);

  // Hauteur du bloc collant supérieur, pour caler le thead dessous.
  // Observée plutôt que calculée : la barre de filtres change de
  // hauteur quand ses boutons passent à la ligne, et un thead posé sur
  // une hauteur figée se retrouverait alors à chevaucher ou à flotter.
  const enteteRef = useRef<HTMLDivElement>(null);
  const [hauteurEntete, setHauteurEntete] = useState(0);

  useLayoutEffect(() => {
    const el = enteteRef.current;
    if (!el) return;
    const mesurer = () => setHauteurEntete(el.offsetHeight);
    mesurer();
    // ResizeObserver manque à jsdom : les tests n'ont pas de mise en
    // page, la hauteur y reste donc à zéro, ce qui est sans effet sur
    // ce qu'ils vérifient.
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(mesurer);
    ro.observe(el);
    return () => ro.disconnect();
    // Aucune dépendance : l'observateur suit toutes les variations de
    // hauteur, d'où qu'elles viennent — filtres repliés, barre de
    // pagination apparue, fenêtre rétrécie.
  }, []);

  const sorted = useMemo(() => {
    if (!sort) return rows;
    const extract = columns[sort.column]?.sortValue;
    if (!extract) return rows;
    const signe = sort.dir === 'asc' ? 1 : -1;
    // Copie avant tri : `rows` appartient à l'appelant, et le store le
    // mémoïse — le muter ferait diverger la donnée de son affichage.
    return [...rows].sort((a, b) => signe * compare(extract(a), extract(b)));
  }, [rows, sort, columns]);

  // La page courante est bornée à la volée plutôt que remise à zéro par
  // un effet : un filtre qui raccourcit la liste doit ramener à la
  // dernière page existante, pas déclencher un second rendu.
  const pageCount = taille ? Math.max(1, Math.ceil(sorted.length / taille)) : 1;
  const pageCourante = Math.min(page, pageCount - 1);
  const visibles = taille
    ? sorted.slice(pageCourante * taille, (pageCourante + 1) * taille)
    : sorted;

  // Les paliers viennent de la taille initiale, jamais de la taille
  // courante : « tout afficher » ne doit pas vider la liste des choix
  // qui permettent d'en sortir.
  const paliers = [...new Set([...pageSizeOptions, pageSize ?? 0])]
    .filter((n) => n > 0)
    .sort((a, b) => a - b);
  // La barre reste tant que la liste dépasse le plus petit palier, même
  // quand tout tient sur une page : sinon, choisir 100 ferait disparaître
  // le sélecteur qui vient de servir, sans moyen de revenir à 10.
  const barreVisible = pageSize !== undefined && sorted.length > paliers[0];

  if (loading && rows.length === 0) {
    return (
      <div className="rounded-panel border border-zinc-200 p-6 dark:border-zinc-800">
        <Skeleton count={5} />
      </div>
    );
  }
  if (rows.length === 0) {
    return <>{emptyState}</>;
  }

  /**
   * Cycle à trois temps : croissant, décroissant, puis retour à l'ordre
   * fourni. Ce troisième temps compte ici — l'ordre naturel des listes
   * Paysim est « plus récent d'abord », et sans lui un clic malheureux
   * le perdrait jusqu'au rechargement de l'écran.
   */
  /**
   * Change le nombre de lignes par page en gardant sous les yeux la
   * première ligne affichée. Revenir en page 1 ferait perdre l'endroit
   * où l'on était, alors que le geste vise justement à en voir plus.
   *
   * `n === 0` vaut « tout afficher » : la table cesse de découper, et la
   * barre reste pour permettre d'y revenir.
   */
  function changerTaille(n: number) {
    setPage(n === 0 ? 0 : Math.floor((pageCourante * (taille ?? n)) / n));
    setTaille(n === 0 ? undefined : n);
  }

  function basculer(i: number) {
    setPage(0);
    setSort((s) => {
      if (!s || s.column !== i) return { column: i, dir: 'asc' };
      if (s.dir === 'asc') return { column: i, dir: 'desc' };
      return null;
    });
  }

  /**
   * La barre de navigation, rendue une seule fois, en tête du bloc
   * collant.
   *
   * Elle était auparavant dupliquée en bas : sur une page pleine, celle
   * du haut sortait de l'écran au moment où l'on décidait de changer de
   * page. Depuis qu'elle reste visible en défilant, la seconde n'a plus
   * d'objet — et deux barres identiques à l'écran valent moins qu'une
   * qui ne s'en va pas.
   *
   * `bordure` est le seul écart entre les deux : chacune se sépare de la
   * table du côté où elle la touche.
   */
  function barre(bordure: 'border-t' | 'border-b') {
    return (
      <div
        className={
          `flex items-center justify-between ${bordure} border-zinc-200 bg-zinc-50 ` +
          'px-4 py-2 text-xs text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400'
        }
      >
        <div className="flex items-center gap-3">
          <span className="tabular">
            {t('common.table.range', {
              from: taille ? pageCourante * taille + 1 : 1,
              to: taille ? pageCourante * taille + visibles.length : sorted.length,
              total: sorted.length,
            })}
          </span>
          <select
            aria-label={t('common.table.pageSize')}
            value={taille ?? 0}
            onChange={(e) => changerTaille(Number(e.target.value))}
            className="rounded border border-zinc-200 bg-transparent px-1 py-0.5 tabular dark:border-zinc-700"
          >
            {paliers.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
            {/* 0 code « pas de découpage » — la valeur du select est
                numérique, et undefined ne se transporte pas dans un
                attribut value. */}
            <option value={0}>{t('common.table.all')}</option>
          </select>
        </div>
        <div className="flex items-center gap-2">
          <BoutonPage
            onClick={() => setPage(pageCourante - 1)}
            disabled={pageCourante === 0}
            label={t('common.table.previous')}
          />
          <span className="tabular">
            {t('common.table.page', { page: pageCourante + 1, count: pageCount })}
          </span>
          <BoutonPage
            onClick={() => setPage(pageCourante + 1)}
            disabled={pageCourante >= pageCount - 1}
            label={t('common.table.next')}
          />
        </div>
      </div>
    );
  }

  return (
    // Pas d'overflow-hidden : il neutraliserait le collage de tout ce
    // qu'il contient. Les angles sont donc arrondis sur les blocs
    // extrêmes plutôt que découpés par le conteneur.
    <div className="rounded-panel border border-zinc-200 dark:border-zinc-800">
      {/*
        Un seul bloc collant pour la barre de filtres et la pagination :
        empilés dans le même conteneur, ils se placent l'un sous l'autre
        sans qu'on ait à connaître leur hauteur. Sous le bandeau de
        navigation, qui colle déjà à top-0 sur 3,5 rem.
      */}
      <div
        ref={enteteRef}
        className="sticky top-14 z-20 rounded-t-panel bg-white dark:bg-zinc-950"
      >
        {toolbar}
        {barreVisible && barre('border-b')}
      </div>
      <table className="w-full text-sm">
        <thead
          // Le thead colle sous ce bloc, à sa hauteur mesurée : la coder
          // en dur se dérèglerait dès que la barre passe à la ligne —
          // ce qui arrive sur écran étroit.
          className="sticky z-10"
          style={{ top: `calc(3.5rem + ${hauteurEntete}px)` }}
        >
          <tr className="border-b border-zinc-200 bg-zinc-50 text-left text-xs font-medium uppercase tracking-wider text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
            {columns.map((col, i) => {
              const triable = !!col.sortValue;
              const actif = sort?.column === i;
              return (
                <th
                  key={i}
                  aria-sort={actif ? (sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}
                  className={
                    'px-4 py-2 ' +
                    (col.align === 'right' ? 'text-right tabular' : '') +
                    (col.srOnly ? ' sr-only' : '')
                  }
                >
                  {triable ? (
                    <button
                      type="button"
                      onClick={() => basculer(i)}
                      className={
                        'inline-flex items-center gap-1 uppercase tracking-wider hover:text-zinc-800 dark:hover:text-zinc-200 ' +
                        (col.align === 'right' ? 'flex-row-reverse' : '')
                      }
                    >
                      {col.header}
                      {actif ? (
                        sort.dir === 'asc' ? (
                          <ChevronUp size={12} />
                        ) : (
                          <ChevronDown size={12} />
                        )
                      ) : (
                        <ChevronsUpDown size={12} className="opacity-40" />
                      )}
                    </button>
                  ) : (
                    col.header
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {visibles.map((row) => (
            <tr
              key={rowKey(row)}
              className="border-b border-zinc-200 last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900/50"
            >
              {columns.map((col, i) => (
                <td
                  key={i}
                  className={
                    'px-4 py-2.5 ' +
                    (col.align === 'right' ? 'text-right' : '') +
                    (col.className ? ' ' + col.className : '')
                  }
                >
                  {col.cell(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>

    </div>
  );
}

/** Bouton de navigation, désactivé en bout de course plutôt que masqué. */
function BoutonPage({
  onClick,
  disabled,
  label,
}: {
  onClick: () => void;
  disabled: boolean;
  label: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="rounded border border-zinc-200 px-2 py-1 hover:bg-zinc-100 disabled:cursor-not-allowed disabled:opacity-40 dark:border-zinc-700 dark:hover:bg-zinc-800"
    >
      {label}
    </button>
  );
}

/**
 * Compare deux clés de tri. Les nombres se comparent numériquement, tout
 * le reste passe par localeCompare — un tri d'identifiants ou de libellés
 * accentués suit alors l'ordre attendu de la langue, pas celui des points
 * de code.
 */
function compare(a: string | number, b: string | number): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  return String(a).localeCompare(String(b));
}
