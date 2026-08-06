// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Link, isRouteErrorResponse, useRouteError } from 'react-router';
import { useT } from '@/shared/i18n/useT';

/**
 * Écran affiché quand le rendu d'une route lève une exception.
 *
 * Sans lui, react-router laisse remonter l'erreur et l'arbre React est
 * démonté : l'utilisateur ne voit qu'une page blanche, sans le moindre
 * indice sur ce qui s'est passé ni moyen de repartir. Un simulateur
 * qu'on utilise pour déboguer autre chose ne peut pas se permettre ça.
 *
 * Le détail technique est affiché tel quel plutôt que masqué derrière
 * un message générique : ici le lecteur est un développeur, et le
 * message d'erreur est précisément ce qu'il cherche.
 */
export function RouteError() {
  const t = useT();
  const error = useRouteError();

  const detail = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : error instanceof Error
      ? error.message
      : String(error);

  return (
    <div className="mx-auto max-w-2xl px-6 py-16 text-center">
      <h1 className="mb-2 text-lg font-semibold text-zinc-900 dark:text-zinc-100">
        {t('routeError.title')}
      </h1>
      <p className="mb-6 text-sm text-zinc-600 dark:text-zinc-400">
        {t('routeError.description')}
      </p>
      <pre className="mb-6 overflow-x-auto rounded border border-zinc-200 bg-zinc-50 p-3 text-left font-mono text-xs text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300">
        {detail}
      </pre>
      <Link
        to="/"
        className="inline-block rounded bg-brand-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-brand-700"
      >
        {t('routeError.backHome')}
      </Link>
    </div>
  );
}
