// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import type { EventEntry, PaymentDetail, PaymentSummary } from '../../../shared/model';

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
  payments: Record<string, PaymentInStore>;
  listLoaded: boolean; // true après un premier setList
  setList: (payments: PaymentSummary[]) => void;
  upsert: (payment: PaymentSummary) => void;
  setDetail: (detail: PaymentDetail) => void;
  remove: (uuid: string) => void;
  clear: () => void;
}

export const usePaymentStore = create<PaymentState>((set) => ({
  payments: {},
  listLoaded: false,
  setList: (payments) =>
    set((s) => {
      const next: Record<string, PaymentInStore> = {};
      for (const p of payments) {
        // Préserve les events déjà chargés si le paiement était déjà
        // en cache — évite un refetch inutile de la page détail juste
        // parce qu'on a rafraîchi la liste.
        const existing = s.payments[p.uuid];
        next[p.uuid] = existing?.events ? { ...p, events: existing.events } : p;
      }
      return { payments: next, listLoaded: true };
    }),
  upsert: (payment) =>
    set((s) => {
      const existing = s.payments[payment.uuid];
      return {
        payments: {
          ...s.payments,
          [payment.uuid]: existing?.events
            ? { ...payment, events: existing.events }
            : payment,
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
