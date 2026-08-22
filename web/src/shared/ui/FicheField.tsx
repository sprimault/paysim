// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import React from 'react';

/**
 * Primitives de présentation des fiches de détail.
 *
 * Les trois fiches — paiement, moyen de paiement, abonnement — sont
 * lues côte à côte pendant un débogage : un titre de section ou un
 * couple libellé/valeur qui ne s'aligne pas d'une fiche à l'autre force
 * l'œil à se recaler à chaque changement d'écran. Elles partageaient
 * déjà exactement le même rendu, mais par copie : trois exemplaires de
 * `Titre`, deux de `Field`, tous identiques au caractère près.
 *
 * Regroupées ici pour que la prochaine retouche de style s'applique aux
 * trois d'un coup, plutôt qu'à celle qu'on a sous les yeux.
 */

/** SectionTitle coiffe un bloc d'une fiche de détail. */
export function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
      {children}
    </h3>
  );
}

/**
 * Field rend un couple libellé/valeur dans une liste de définition.
 *
 * `wide` fait occuper les deux colonnes de la grille — pour une valeur
 * qui ne tient pas sur une demi-largeur, un identifiant long ou une
 * charge utile.
 */
export function Field({
  label,
  value,
  wide,
}: {
  label: string;
  value: React.ReactNode;
  wide?: boolean;
}) {
  return (
    <div className={wide ? 'sm:col-span-2' : ''}>
      <dt className="text-xs uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
        {label}
      </dt>
      <dd className="mt-0.5 text-sm text-zinc-900 dark:text-zinc-100">{value}</dd>
    </div>
  );
}
