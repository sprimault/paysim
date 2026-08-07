// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import type { EventEntry, PaymentDetail, PaymentSummary } from '@/shared/model';

/**
 * Store de l'entité Payment. Un seul objet par UUID : le détail
 * (events) est optionnel — présent quand la page détail a fetché,
 * absent quand seule la liste est chargée. Pas de duplication
 * liste/détail : la même entrée grandit à mesure qu'elle est
 * demandée.
 *
 * État interne : Record indexé par uuid pour l'accès O(1) et un
 * ordering stable ne dépendant pas de l'insertion (l'UI trie via
 * selector). Zustand fait la comparaison référentielle : chaque
 * mutation retourne un nouveau Record via l'opérateur spread.
 */

export type PaymentInStore = PaymentSummary & { events?: EventEntry[] };

interface PaymentState {
  /** Paiements indexés par uuid — un seul exemplaire par paiement. */
  payments: Record<string, PaymentInStore>;
  /**
   * Passe à true au premier setList. Distingue « liste vide » de
   * « liste pas encore chargée », que le rendu ne doit pas confondre :
   * l'un affiche un état vide, l'autre un squelette.
   */
  listLoaded: boolean;
  /** Remplace la liste, en préservant les events déjà chargés. */
  setList: (payments: PaymentSummary[]) => void;
  /** Insère ou met à jour un paiement, sans toucher à ses events. */
  upsert: (payment: PaymentSummary) => void;
  /** Enregistre le détail complet, events compris. */
  setDetail: (detail: PaymentDetail) => void;
  /** Retire un paiement du cache. */
  remove: (uuid: string) => void;
  /** Vide le cache — après une purge ou une réinitialisation. */
  clear: () => void;
}

export const usePaymentStore = create<PaymentState>((set) => ({
  payments: {},
  listLoaded: false,
  setList: (payments) =>
    set((s) => {
      const next: Record<string, PaymentInStore> = {};
      for (const p of payments) {
        // Le résumé rafraîchit ce qu'il porte et ne touche pas au reste :
        // events, customer, metadata ne viennent que du détail, et une
        // liste rechargée ne doit pas les effacer.
        //
        // Nommer les champs à préserver un par un s'est révélé fragile —
        // customer et metadata, ajoutés plus tard, disparaissaient dès
        // que le Header rafraîchissait la liste pour ses compteurs.
        const existing = s.payments[p.uuid];
        next[p.uuid] = existing ? { ...existing, ...p } : p;
      }
      return { payments: next, listLoaded: true };
    }),
  upsert: (payment) =>
    set((s) => {
      const existing = s.payments[payment.uuid];
      return {
        payments: {
          ...s.payments,
          // Même règle que setList : le résumé complète, il n'efface pas.
          [payment.uuid]: existing ? { ...existing, ...payment } : payment,
        },
      };
    }),
  setDetail: (detail) =>
    set((s) => ({
      payments: { ...s.payments, [detail.uuid]: detail },
    })),
  remove: (uuid) =>
    set((s) => {
      if (!(uuid in s.payments)) return s;
      const next = { ...s.payments };
      delete next[uuid];
      return { payments: next };
    }),
  clear: () => set({ payments: {}, listLoaded: false }),
}));

/**
 * paymentListSelector renvoie la liste triée par updatedAt décroissant
 * — le plus récent d'abord, cohérent avec un journal d'activité
 * temps réel.
 */
export function paymentListSelector(s: PaymentState): PaymentInStore[] {
  return Object.values(s.payments).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
}
