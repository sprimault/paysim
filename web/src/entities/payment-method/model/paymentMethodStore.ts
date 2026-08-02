// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import type { PaymentMethodOutput } from '@/shared/model';

/**
 * Store de l'entité PaymentMethod. Un seul objet par Token — la vue
 * complète tient dans un `PaymentMethodOutput`, pas de séparation
 * liste/détail comme pour payment.
 */

interface PaymentMethodState {
  methods: Record<string, PaymentMethodOutput>;
  listLoaded: boolean;
  setList: (methods: PaymentMethodOutput[]) => void;
  upsert: (m: PaymentMethodOutput) => void;
  remove: (token: string) => void;
  clear: () => void;
}

export const usePaymentMethodStore = create<PaymentMethodState>((set) => ({
  methods: {},
  listLoaded: false,
  setList: (methods) =>
    set(() => {
      const next: Record<string, PaymentMethodOutput> = {};
      for (const m of methods) {
        next[m.token] = m;
      }
      return { methods: next, listLoaded: true };
    }),
  upsert: (m) =>
    set((s) => ({ methods: { ...s.methods, [m.token]: m } })),
  remove: (token) =>
    set((s) => {
      const next = { ...s.methods };
      delete next[token];
      return { methods: next };
    }),
  clear: () => set({ methods: {}, listLoaded: false }),
}));
