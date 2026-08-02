// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import type { SubscriptionOutput } from '@/shared/model';

/**
 * Store de l'entité Subscription. Un seul objet par ID — pas de
 * séparation liste/détail comme pour payment, l'API retourne déjà la
 * vue complète (pas d'events séparés à charger).
 *
 * Zustand fait la comparaison référentielle : chaque mutation retourne
 * un nouveau Record via l'opérateur spread.
 */

interface SubscriptionState {
  subscriptions: Record<string, SubscriptionOutput>;
  listLoaded: boolean;
  setList: (subs: SubscriptionOutput[]) => void;
  upsert: (sub: SubscriptionOutput) => void;
  remove: (id: string) => void;
  clear: () => void;
}

export const useSubscriptionStore = create<SubscriptionState>((set) => ({
  subscriptions: {},
  listLoaded: false,
  setList: (subs) =>
    set(() => {
      const next: Record<string, SubscriptionOutput> = {};
      for (const s of subs) {
        next[s.id] = s;
      }
      return { subscriptions: next, listLoaded: true };
    }),
  upsert: (sub) =>
    set((s) => ({
      subscriptions: { ...s.subscriptions, [sub.id]: sub },
    })),
  remove: (id) =>
    set((s) => {
      const next = { ...s.subscriptions };
      delete next[id];
      return { subscriptions: next };
    }),
  clear: () => set({ subscriptions: {}, listLoaded: false }),
}));
