// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useSSE } from './useSSE';
import { fetchPayment, fetchPayments } from '@/entities/payment/api/paymentApi';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import { fetchWebhooks } from '@/entities/webhook/api/webhookApi';
import { useWebhookStore } from '@/entities/webhook/model/webhookStore';
import { fetchPaymentMethods } from '@/entities/payment-method/api/paymentMethodApi';
import { usePaymentMethodStore } from '@/entities/payment-method/model/paymentMethodStore';
import { fetchSubscriptions } from '@/entities/subscription/api/subscriptionApi';
import { useSubscriptionStore } from '@/entities/subscription/model/subscriptionStore';
import { isPaysimEvent } from '@/shared/model/events';

/**
 * resynchroniser relit les collections déjà chargées.
 *
 * Appelé au retour d'une coupure SSE. Le rattrapage par `Last-Event-ID`
 * ne suffit pas : après un redémarrage serveur, le ring d'événements est
 * vide et les identifiants repartent, si bien que le client reçoit le
 * flux vivant sans jamais apprendre que ce qu'il affiche a disparu.
 * `internal/bus` prévoit explicitement que le front relise un instantané
 * dans ce cas.
 *
 * Seules les collections déjà chargées sont relues — en charger une que
 * l'utilisateur n'a jamais ouverte reviendrait à travailler pour un
 * écran que personne ne regarde.
 */
function resynchroniser(): void {
  const payments = usePaymentStore.getState();
  if (payments.listLoaded) {
    void fetchPayments().then(payments.setList).catch(() => undefined);
  }
  const webhooks = useWebhookStore.getState();
  if (webhooks.listLoaded) {
    void fetchWebhooks().then(webhooks.setList).catch(() => undefined);
  }
  const methods = usePaymentMethodStore.getState();
  if (methods.listLoaded) {
    void fetchPaymentMethods().then(methods.setList).catch(() => undefined);
  }
  const subscriptions = useSubscriptionStore.getState();
  if (subscriptions.listLoaded) {
    void fetchSubscriptions().then(subscriptions.setList).catch(() => undefined);
  }
}

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

  const removePayment = usePaymentStore((s) => s.remove);
  const setPaymentList = usePaymentStore((s) => s.setList);

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
      case 'payment_deleted':
        // Retire directement du store — pas de refetch nécessaire.
        removePayment(raw.data.uuid);
        return;
      case 'payments_purged':
        // Refetch la liste plutôt qu'un clear local : après un bulk
        // delete, on veut être sûr qu'aucune entrée ne survit du fait
        // d'un race entre plusieurs clients.
        void fetchPayments().then(setPaymentList).catch(() => undefined);
        return;
      case 'webhook_enqueued':
      case 'webhook_delivered':
      case 'webhook_failed':
        // Rafales possibles sur les webhooks — refetch de la liste
        // plutôt qu'unitaire évite le cascade d'appels.
        void fetchWebhooks().then(setWebhookList).catch(() => undefined);
        return;
    }
  }, resynchroniser);
}
