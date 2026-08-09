// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useLayoutEffect, useState, type RefObject } from 'react';

/**
 * Élément d'ancrage : l'élément cliqué lui-même, plutôt qu'une
 * référence. Les déclencheurs ne sont pas toujours dans le composant qui
 * ouvre la boîte — la corbeille d'une ligne de tableau vit dans la
 * ligne, et remonte l'intention au parent. Capturer `currentTarget` au
 * clic marche dans les deux cas, sans imposer de forwardRef aux boutons.
 */
export type AnchorElement = HTMLElement | null | undefined;

/** Marge entre le déclencheur et la boîte, et bord de fenêtre minimal. */
const ecart = 8;

export interface AnchoredPosition {
  /** Coordonnées en pixels, ou null tant que rien n'est mesurable. */
  top: number;
  left: number;
}

/**
 * useAnchoredPosition place une boîte sous l'élément qui l'a ouverte.
 *
 * Le placement se décide en trois temps, dans cet ordre :
 *
 *   1. sous le déclencheur, aligné sur son bord gauche ;
 *   2. au-dessus, si la boîte déborderait le bas de la fenêtre ;
 *   3. centré, si aucune des deux positions ne tient.
 *
 * La troisième branche n'est pas une politesse : sur une fenêtre courte,
 * une boîte ancrée déborde des deux côtés et ses boutons deviennent
 * inatteignables. Mieux vaut alors renoncer à l'ancrage que rendre
 * l'action impossible.
 *
 * Le calcul est refait au défilement et au redimensionnement — un
 * ancrage figé sur des coordonnées d'ouverture se décale dès que la page
 * bouge, ce qui est pire qu'un centrage.
 *
 * Retourne null quand aucun déclencheur n'est fourni : l'appelant
 * retombe alors sur le centrage, sans branche particulière.
 */
export function useAnchoredPosition(
  anchorEl: AnchorElement,
  boxRef: RefObject<HTMLElement | null>,
  open: boolean,
): AnchoredPosition | null {
  const [position, setPosition] = useState<AnchoredPosition | null>(null);

  useLayoutEffect(() => {
    if (!open || !anchorEl) {
      setPosition(null);
      return;
    }

    function calculer() {
      const ancre = anchorEl;
      const boite = boxRef.current;
      if (!ancre || !boite) return;

      const a = ancre.getBoundingClientRect();
      const largeur = boite.offsetWidth;
      const hauteur = boite.offsetHeight;
      const vw = window.innerWidth;
      const vh = window.innerHeight;

      const placeDessous = vh - a.bottom - ecart;
      const placeDessus = a.top - ecart;

      // Ni dessous ni dessus ne suffit : on abandonne l'ancrage.
      if (hauteur > placeDessous && hauteur > placeDessus) {
        setPosition(null);
        return;
      }

      const top =
        hauteur <= placeDessous ? a.bottom + ecart : a.top - hauteur - ecart;

      // Aligné sur le bord gauche du déclencheur, puis ramené dans la
      // fenêtre : un bouton proche du bord droit pousserait sinon la
      // boîte hors champ.
      const left = Math.min(Math.max(ecart, a.left), vw - largeur - ecart);

      setPosition({ top, left });
    }

    calculer();
    window.addEventListener('scroll', calculer, true);
    window.addEventListener('resize', calculer);
    return () => {
      window.removeEventListener('scroll', calculer, true);
      window.removeEventListener('resize', calculer);
    };
  }, [anchorEl, boxRef, open]);

  return position;
}
