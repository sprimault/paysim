// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';
import { Skeleton } from '@/shared/ui/Skeleton';

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
}

export interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[];
  /** Extracteur d'identifiant unique pour la clé React. */
  rowKey: (row: T) => string;
  /** Contenu rendu quand rows est vide et loading est false. */
  emptyState?: ReactNode;
  /** Affiche un Skeleton tant que loading + rows vide. */
  loading?: boolean;
}

/**
 * Table dense et sobre — pas de cards, pas d'ombre. Aligné sur le
 * pattern PaymentList existant, factorisé pour subscriptions et
 * payment-methods (règle de trois anticipée : 3 usages prévus).
 * L'header, la sélection Tailwind, les hover-rows et les bordures
 * dark: sont figés ici pour rester cohérents à travers l'app.
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  emptyState,
  loading,
}: DataTableProps<T>) {
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
  return (
    <div className="overflow-hidden rounded-panel border border-zinc-200 dark:border-zinc-800">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-zinc-200 bg-zinc-50 text-left text-xs font-medium uppercase tracking-wider text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
            {columns.map((col, i) => (
              <th
                key={i}
                className={
                  'px-4 py-2 ' +
                  (col.align === 'right' ? 'text-right tabular' : '') +
                  (col.srOnly ? ' sr-only' : '')
                }
              >
                {col.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
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
