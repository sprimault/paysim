// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Helpers spécifiques au protocole PayZen V4 côté UI. Isolés en
 * fonction pure pour être testables sans monter un composant.
 */

export interface ParsedPayzenBody {
  /**
   * Contenu de kr-answer désérialisé, ou null si le champ est absent
   * ou illisible — un corps altéré par le chaos ne doit pas casser
   * l'affichage.
   */
  krAnswer: unknown | null;
  /**
   * Les autres champs du formulaire, kr-hash en tête. Conservés bruts
   * pour que la vue puisse montrer ce qui a réellement été transmis.
   */
  rest: Record<string, string>;
}

/**
 * parsePayzenBody décode un body form-urlencoded envoyé par un
 * webhook PayZen. Le champ kr-answer est parsé en JSON quand possible ;
 * il est renvoyé tel quel (string) sinon. Les autres champs
 * (kr-hash, kr-hash-algorithm, kr-hash-key, kr-answer-type) sont
 * exposés séparément dans `rest`.
 */
export function parsePayzenBody(body: string): ParsedPayzenBody {
  const params = new URLSearchParams(body);
  let krAnswer: unknown | null = null;
  const rest: Record<string, string> = {};
  for (const [k, v] of params.entries()) {
    if (k === 'kr-answer') {
      try {
        krAnswer = JSON.parse(v);
      } catch {
        krAnswer = v;
      }
    } else {
      rest[k] = v;
    }
  }
  return { krAnswer, rest };
}
