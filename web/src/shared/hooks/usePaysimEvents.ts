// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useSSE } from './useSSE';
import { fetchPayment, fetchPayments } from '@/entities/payment/api/paymentApi';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import { fetchWebhooks } from '@/entities/webhook/api/webhookApi';
import { useWebhookStore } from '@/entities/webhook/model/webhookStore';
import { isPaysimEvent } from '@/shared/model/events';

/**
 * usePaysimEvents ouvre UNE connexion SSE au top level de l'app et
 * dispatche les events vers les stores payment et webhook. Un seul
 * EventSource pour tout le front — évite d'ouvrir une connexion par
 * entity qui écoute.
 *
 * Stratégie : sur chaque event, refetch la ressource concernée. Le
 * payload SSE est trop léger pour reconstruire un DTO complet
 * (payment_created n'inclut ni state ni dates, payment_state_changed
 * n'inclut pas l'amount, etc.). Le coût d'un GET local par event est
 * négligeable et garantit la cohérence sans coordination fragile.
 *
 * Retourne `connected` — la seule connexion SSE de l'app, celle qui
 * pilote l'indicateur du Header.
 */
export function usePaysimEvents(
  streamPath = '/paysim/api/v1/events/stream',
): { connected: boolean } {
  const setPaymentDetail = usePaymentStore((s) => s.setDetail);
  const upsertPayment = usePaymentStore((s) => s.upsert);
  const setWebhookList = useWebhookStore((s) => s.setList);

  return useSSE(streamPath, (raw) => {
    if (!isPaysimEvent(raw)) return;

    switch (raw.type) {
      case 'payment_created':
      case 'payment_state_changed': {
        const uuid = raw.data.uuid;
        fetchPayment(uuid)
          .then(setPaymentDetail)
          .catch(() => {
            // Fallback : au moins upserter le résumé depuis la liste
            // pour ne pas laisser un event orphelin invisible dans l'UI.
            void fetchPayments().then((list) => {
              const p = list.find((x) => x.uuid === uuid);
              if (p) upsertPayment(p);
            });
          });
        return;
      }
      case 'webhook_enqueued':
      case 'webhook_delivered':
      case 'webhook_failed':
        // Rafales possibles sur les webhooks — refetch de la liste
        // plutôt qu'unitaire évite le cascade d'appels.
        void fetchWebhooks().then(setWebhookList).catch(() => undefined);
        return;
    }
  });
}
