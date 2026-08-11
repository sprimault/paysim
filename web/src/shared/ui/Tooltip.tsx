// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

/** Côté où la boîte s'est placée, et donc où pointe le chevron. */
type Cote = 'haut' | 'bas';

interface Placement {
  top: number;
  left: number;
  cote: Cote;
  /** Abscisse du chevron dans la boîte, pour qu'il vise l'ancre. */
  flecheLeft: number;
}

const MARGE = 8; // écart entre l'ancre et la boîte
const BORD = 8; // distance minimale aux bords de la fenêtre

/**
 * Tooltip — une infobulle lisible, à la place de l'attribut `title`.
 *
 * Le `title` natif suffisait fonctionnellement mais pas à l'usage : il
 * met une seconde à apparaître, se place où le navigateur veut, et son
 * curseur d'accompagnement peut recouvrir le texte qu'on essaie de lire.
 *
 * Rendu dans un portail plutôt qu'en position absolue : les tableaux de
 * l'interface vivent dans un conteneur `overflow-hidden`, qui
 * découperait une boîte débordant de la ligne.
 *
 * Au-dessus par défaut, en dessous quand le haut manque de place — le
 * chevron suit et désigne toujours l'ancre. Recalé horizontalement pour
 * rester dans la fenêtre, le chevron gardant sa visée.
 */
export function Tooltip({
  label,
  children,
  focusExterne = false,
}: {
  label?: string;
  children: ReactNode;
  /**
   * Le focus est déjà géré autour de l'infobulle — par l'enfant (un
   * bouton, un lien) ou par un ancêtre (l'infobulle est posée dans un
   * lien). L'enveloppe ne pose alors ni `tabIndex` ni `aria-label`.
   *
   * Deux raisons, et la seconde est bloquante : un contrôle qui prend
   * deux arrêts au clavier et porte deux noms accessibles dessert plus
   * qu'il n'aide ; et un élément focusable **à l'intérieur d'un `<a>`**
   * est du contenu interactif imbriqué, que HTML5 interdit.
   *
   * L'infobulle apparaît quand même au focus : React propage `focus` et
   * `blur` depuis l'enfant, contrairement au DOM natif.
   */
  focusExterne?: boolean;
}) {
  const [visible, setVisible] = useState(false);
  const ancreRef = useRef<HTMLSpanElement>(null);
  const boiteRef = useRef<HTMLDivElement>(null);
  const [placement, setPlacement] = useState<Placement | null>(null);

  useLayoutEffect(() => {
    if (!visible) {
      setPlacement(null);
      return;
    }

    function calculer() {
      const ancre = ancreRef.current;
      const boite = boiteRef.current;
      if (!ancre || !boite) return;

      const a = ancre.getBoundingClientRect();
      const largeur = boite.offsetWidth;
      const hauteur = boite.offsetHeight;

      // Au-dessus sauf si ça sort de l'écran : mieux vaut basculer que
      // laisser la boîte à moitié hors champ.
      const cote: Cote = a.top - hauteur - MARGE >= BORD ? 'haut' : 'bas';
      const top = cote === 'haut' ? a.top - hauteur - MARGE : a.bottom + MARGE;

      // Centrée sur l'ancre, puis ramenée dans la fenêtre.
      const centre = a.left + a.width / 2;
      const left = Math.min(
        Math.max(centre - largeur / 2, BORD),
        Math.max(window.innerWidth - largeur - BORD, BORD),
      );

      // Le chevron vise l'ancre même quand la boîte a été recalée, et
      // reste dans ses angles arrondis.
      const flecheLeft = Math.min(Math.max(centre - left, 12), Math.max(largeur - 12, 12));

      setPlacement({ top, left, cote, flecheLeft });
    }

    calculer();
    // Le survol ne survit pas à un défilement ou à un redimensionnement,
    // mais les deux peuvent se produire pendant : on suit plutôt que de
    // laisser la boîte en arrière.
    window.addEventListener('scroll', calculer, true);
    window.addEventListener('resize', calculer);
    return () => {
      window.removeEventListener('scroll', calculer, true);
      window.removeEventListener('resize', calculer);
    };
  }, [visible]);

  // Sans libellé, on ne rend qu'un passe-plat : ni curseur, ni écouteur,
  // ni boîte vide au survol.
  if (!label) return <>{children}</>;

  return (
    <>
      <span
        ref={ancreRef}
        className="inline-flex cursor-pointer items-center gap-1.5"
        onMouseEnter={() => setVisible(true)}
        onMouseLeave={() => setVisible(false)}
        onFocus={() => setVisible(true)}
        onBlur={() => setVisible(false)}
        {...(focusExterne
          ? {}
          : // Lu par les lecteurs d'écran, pour qui le survol n'existe
            // pas, et atteignable au clavier.
            { tabIndex: 0, 'aria-label': label })}
      >
        {children}
      </span>

      {visible &&
        createPortal(
          <div
            ref={boiteRef}
            role="tooltip"
            className="pointer-events-none fixed z-50 max-w-xs rounded-md bg-zinc-900 px-2.5 py-1.5 text-xs font-medium text-zinc-50 shadow-lg dark:bg-zinc-100 dark:text-zinc-900"
            style={
              placement
                ? { top: placement.top, left: placement.left }
                : // Premier rendu : hors champ le temps de mesurer, sinon
                  // la boîte clignote en haut à gauche.
                  { top: -9999, left: -9999 }
            }
          >
            {label}
            {placement && (
              <span
                className="absolute h-2 w-2 rotate-45 bg-zinc-900 dark:bg-zinc-100"
                style={{
                  left: placement.flecheLeft - 4,
                  ...(placement.cote === 'haut' ? { bottom: -4 } : { top: -4 }),
                }}
              />
            )}
          </div>,
          document.body,
        )}
    </>
  );
}
