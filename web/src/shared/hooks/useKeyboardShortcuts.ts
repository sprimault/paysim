// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef } from 'react';

/**
 * Délai au bout duquel un préfixe de séquence est oublié. Assez long
 * pour une frappe humaine en deux temps, assez court pour qu'un `g`
 * tapé par erreur ne détourne pas la touche suivante.
 */
const SEQUENCE_TIMEOUT_MS = 1500;

/**
 * Un raccourci clavier : soit une touche unique (`'?'`, `'r'`), soit
 * une séquence de deux touches façon Vim (`['g', 'p']`).
 *
 * La comparaison porte sur `KeyboardEvent.key`, donc la casse est
 * significative : `'R'` exige Shift, `'r'` non. C'est ce qui permet
 * de distinguer une action anodine d'une action destructrice sur la
 * même lettre.
 */
export interface Shortcut {
  /** Touche unique, ou couple préfixe/touche pour une séquence. */
  keys: string | [string, string];
  /** Action déclenchée quand la combinaison est reconnue. */
  run: () => void;
}

/**
 * Enregistre des raccourcis clavier au niveau du document, le temps
 * que le composant appelant reste monté. Plusieurs composants peuvent
 * l'appeler simultanément : chacun installe son propre écouteur et ne
 * réagit qu'à ses propres combinaisons.
 *
 * Trois inhibitions, sans lesquelles les raccourcis nuisent plus
 * qu'ils n'aident :
 *
 *   - **Saisie en cours** — taper un montant dans un champ ne doit
 *     jamais déclencher une navigation. Tout `input`, `textarea`,
 *     `select` ou élément `contenteditable` neutralise le clavier.
 *   - **Modale ouverte** — un dialogue de confirmation capture le
 *     contexte ; laisser passer un raccourci derrière lui reviendrait
 *     à agir sur un écran que l'utilisateur ne regarde pas.
 *   - **Modificateurs** — `Ctrl`, `Alt` et `Meta` appartiennent au
 *     navigateur et au système. On ne détourne que les frappes nues,
 *     Shift excepté puisqu'il sert à produire les majuscules.
 *
 * @param shortcuts Table des raccourcis. Relue à chaque rendu via une
 *   ref, donc les closures capturées restent fraîches sans réinstaller
 *   l'écouteur à chaque frappe de rendu.
 * @param enabled Passe à false pour suspendre l'écoute sans démonter
 *   le composant.
 */
export function useKeyboardShortcuts(shortcuts: Shortcut[], enabled = true): void {
  // Les handlers changent à chaque rendu (closures sur l'état courant),
  // mais réinstaller l'écouteur à chaque fois perdrait le préfixe de
  // séquence en cours de frappe. La ref découple les deux.
  const ref = useRef(shortcuts);
  ref.current = shortcuts;

  useEffect(() => {
    if (!enabled) return;

    let prefix: string | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const oublierPrefixe = () => {
      prefix = null;
      if (timer) clearTimeout(timer);
    };

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if (estEnSaisie(e.target)) return;
      if (document.querySelector('[role="dialog"]')) return;

      const touche = e.key;

      if (prefix) {
        const attendu = prefix;
        oublierPrefixe();
        const trouve = ref.current.find(
          (s) => Array.isArray(s.keys) && s.keys[0] === attendu && s.keys[1] === touche,
        );
        if (trouve) {
          e.preventDefault();
          trouve.run();
        }
        return;
      }

      const simple = ref.current.find((s) => s.keys === touche);
      if (simple) {
        e.preventDefault();
        simple.run();
        return;
      }

      // Aucune correspondance directe : la touche ouvre peut-être une
      // séquence. On l'arme sans consommer l'événement, pour ne pas
      // bloquer un raccourci natif si la suite ne vient jamais.
      if (ref.current.some((s) => Array.isArray(s.keys) && s.keys[0] === touche)) {
        prefix = touche;
        timer = setTimeout(oublierPrefixe, SEQUENCE_TIMEOUT_MS);
      }
    };

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      if (timer) clearTimeout(timer);
    };
  }, [enabled]);
}

/**
 * Vrai quand la cible de l'événement accepte du texte au clavier.
 * On interroge la cible plutôt que `document.activeElement` : c'est
 * elle qui recevra la frappe, y compris à l'intérieur d'un shadow DOM.
 */
function estEnSaisie(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  return ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName);
}
