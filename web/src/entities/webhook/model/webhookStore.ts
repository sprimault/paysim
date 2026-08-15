// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import type { WebhookDetail, WebhookEntry } from '@/shared/model';

/**
 * Store de l'entité Webhook. Même pattern que paymentStore : un
 * entry par ID, le détail (headers, body) est optionnel et chargé
 * à la demande.
 */

export type WebhookInStore = WebhookEntry & {
  headers?: Record<string, string>;
  body?: string;
};

interface WebhookState {
  webhooks: Record<string, WebhookInStore>;
  listLoaded: boolean;
  setList: (webhooks: WebhookEntry[]) => void;
  upsert: (webhook: WebhookEntry) => void;
  setDetail: (detail: WebhookDetail) => void;
  remove: (id: string) => void;
  removeByPayment: (paymentUuid: string) => void;
  clear: () => void;
}

export const useWebhookStore = create<WebhookState>((set) => ({
  webhooks: {},
  listLoaded: false,
  setList: (webhooks) =>
    set((s) => {
      const next: Record<string, WebhookInStore> = {};
      for (const w of webhooks) {
        const existing = s.webhooks[w.id];
        next[w.id] =
          existing?.headers || existing?.body ? { ...w, headers: existing.headers, body: existing.body } : w;
      }
      return { webhooks: next, listLoaded: true };
    }),
  upsert: (webhook) =>
    set((s) => {
      const existing = s.webhooks[webhook.id];
      return {
        webhooks: {
          ...s.webhooks,
          [webhook.id]:
            existing?.headers || existing?.body
              ? { ...webhook, headers: existing.headers, body: existing.body }
              : webhook,
        },
      };
    }),
  setDetail: (detail) =>
    set((s) => ({
      webhooks: { ...s.webhooks, [detail.id]: detail },
    })),
  remove: (id) =>
    set((s) => {
      if (!(id in s.webhooks)) return s;
      const next = { ...s.webhooks };
      delete next[id];
      return { webhooks: next };
    }),
  /**
   * Retire les livraisons d'un paiement — le serveur les supprime avec
   * lui, l'écran doit suivre sans attendre un rechargement.
   *
   * La garde sur l'uuid vide n'est pas défensive : les livraisons sans
   * paiement rattaché sont légitimes, et un appel avec une chaîne vide
   * les emporterait toutes. Aujourd'hui le champ arrive `undefined`
   * parce que le serveur l'omet, ce qui masque le problème — jusqu'au
   * jour où il ne l'omettra plus.
   */
  removeByPayment: (paymentUuid) =>
    set((s) => {
      if (!paymentUuid) return s;
      const next: Record<string, WebhookInStore> = {};
      let retire = false;
      for (const [id, w] of Object.entries(s.webhooks)) {
        if (w.paymentUuid === paymentUuid) {
          retire = true;
          continue;
        }
        next[id] = w;
      }
      return retire ? { webhooks: next } : s;
    }),
  clear: () => set({ webhooks: {}, listLoaded: false }),
}));

/**
 * webhookListSelector : plus récent d'abord, sur createdAt.
 */
export function webhookListSelector(s: WebhookState): WebhookInStore[] {
  return Object.values(s.webhooks).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}
