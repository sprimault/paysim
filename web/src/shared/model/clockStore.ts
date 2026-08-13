// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import { apiGetJson, apiPostJson } from '@/shared/api/client';
import type { AdvanceRequest, ClockState } from '@/shared/model';

/**
 * Store de l'horloge du simulateur.
 *
 * L'instance peut être avancée dans le temps. Sans ce store, l'interface
 * daterait tout depuis l'horloge du navigateur : sur une instance
 * avancée de quatre jours, chaque paiement s'afficherait « créé dans
 * 4 jours », plausible et faux, et rien n'expliquerait pourquoi.
 *
 * On retient le décalage plutôt qu'un instant : `now()` reste calculé à
 * l'appel, donc l'affichage continue de vieillir seconde après seconde
 * sans qu'on interroge le serveur. La dérive entre les deux horloges
 * est incluse dans le décalage mesuré, ce qui corrige au passage un
 * poste dont l'heure système est fausse.
 */
interface ClockStore {
  /**
   * Écart entre l'heure du simulateur et celle du navigateur, en
   * millisecondes. Zéro tant que rien n'a été chargé ou avancé.
   */
  decalageMs: number;

  /** Vrai dès que le serveur a répondu au moins une fois. */
  charge: boolean;

  /** Heure vue par le simulateur, recalculée à chaque appel. */
  now: () => Date;

  /** Recharge le décalage depuis le serveur. */
  rafraichir: () => Promise<void>;

  /** Avance l'horloge du simulateur puis recharge le décalage. */
  avancer: (duration: string) => Promise<void>;

  /** Ramène l'instance à l'heure réelle. */
  reinitialiser: () => Promise<void>;
}

/**
 * Racine des routes d'horloge. Écrite en entier : `apiUrl` ne préfixe
 * que le base path, pas la version d'API — un chemin abrégé atteindrait
 * la SPA au lieu de l'API, et la requête réussirait en renvoyant du
 * HTML.
 */
const BASE = '/clock';

/** Convertit une réponse serveur en décalage par rapport au navigateur. */
function decalageDepuis(etat: ClockState): number {
  return new Date(etat.now).getTime() - Date.now();
}

export const useClockStore = create<ClockStore>((set, get) => ({
  decalageMs: 0,
  charge: false,

  now: () => new Date(Date.now() + get().decalageMs),

  rafraichir: async () => {
    const etat = await apiGetJson<ClockState>(BASE);
    set({ decalageMs: decalageDepuis(etat), charge: true });
  },

  avancer: async (duration: string) => {
    const etat = await apiPostJson<AdvanceRequest, ClockState>(`${BASE}/advance`, { duration });
    set({ decalageMs: decalageDepuis(etat), charge: true });
  },

  reinitialiser: async () => {
    const etat = await apiPostJson<null, ClockState>(`${BASE}/reset`, null);
    set({ decalageMs: decalageDepuis(etat), charge: true });
  },
}));

/**
 * Seuil au-delà duquel on considère l'instance décalée.
 *
 * Une seconde : la latence d'un appel et la dérive ordinaire entre deux
 * horloges tiennent dessous, une avance délibérée jamais. Sans ce
 * seuil, le bandeau s'allumerait sur du bruit.
 */
export const SEUIL_DECALAGE_MS = 1000;

/** Vrai quand l'instance ne montre plus l'heure réelle. */
export function estDecalee(decalageMs: number): boolean {
  return Math.abs(decalageMs) >= SEUIL_DECALAGE_MS;
}
