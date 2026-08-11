// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { AlertTriangle } from 'lucide-react';

/**
 * ErrorBanner — l'échec d'un chargement, dit une fois pour toutes.
 *
 * Le même bloc rouge était recopié dans les trois listes, aux mêmes
 * classes près, et deux écrans de détail rendaient une variante pleine
 * page. Recopié, il dérive : une couleur ajustée ici, un `mb-4` oublié
 * là, et l'erreur ne se ressemble plus d'un écran à l'autre.
 *
 * Deux formes, un seul composant :
 *
 *   - en tête de liste, sous les filtres, la table restant visible —
 *     une lecture qui échoue ne doit pas effacer ce qu'on avait déjà ;
 *   - en pleine page sur un détail, où il n'y a rien d'autre à montrer.
 *
 * `role="alert"` : l'échec est annoncé aux lecteurs d'écran sans
 * attendre qu'ils atteignent la zone.
 */
export function ErrorBanner({
  message,
  pleinePage = false,
}: {
  message: string;
  /** Le détail n'a rien d'autre à afficher : on occupe la page. */
  pleinePage?: boolean;
}) {
  if (pleinePage) {
    return (
      <div
        role="alert"
        className="mx-auto flex max-w-4xl items-center gap-2 px-6 py-6 text-sm text-rose-700 dark:text-rose-300"
      >
        <AlertTriangle size={16} className="shrink-0" />
        {message}
      </div>
    );
  }

  return (
    <div
      role="alert"
      className="mb-4 flex items-center gap-2 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300"
    >
      <AlertTriangle size={16} className="shrink-0" />
      {message}
    </div>
  );
}
