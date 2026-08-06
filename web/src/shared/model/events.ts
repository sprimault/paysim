// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Types des événements diffusés par le bus SSE côté serveur
 * (internal/bus, publiés par internal/providers/payzen et
 * internal/delivery). Le format wire est
 *   { type: "<kind>", at: "<ISO>", data: { ... } }
 * — voir api.streamEvents.
 *
 * On typologise les data par kind via une union discriminée pour que
 * le consommateur (usePaymentEvents, useWebhookEvents) puisse
 * narrower proprement dans son switch.
 *
 * Toutes les interfaces ci-dessous partagent la même forme : `type`
 * est le discriminant, `at` l'horodatage ISO de publication, et `data`
 * la charge utile propre à l'événement. Seul `data` est commenté.
 */

// Discriminant complet des events publiés par Paysim v1.
export type PaysimEventType =
  | 'payment_created'
  | 'payment_state_changed'
  | 'payment_deleted'
  | 'payments_purged'
  | 'webhook_enqueued'
  | 'webhook_delivered'
  | 'webhook_failed'
  | 'reset';

/** Un paiement vient d'être créé. Amount est en centimes entiers. */
export interface PaymentCreatedEvent {
  type: 'payment_created';
  at: string;
  data: { uuid: string; orderId: string; amount: number; currency: string };
}

/**
 * Un paiement a changé d'état. `state` est le vocabulaire du domaine,
 * `outcome` celui du provider — les deux coexistent parce qu'ils ne
 * disent pas la même chose : captured contre PAID.
 */
export interface PaymentStateChangedEvent {
  type: 'payment_state_changed';
  at: string;
  data: { uuid: string; orderId: string; state: string; outcome: string };
}

/** Un paiement a été supprimé unitairement. */
export interface PaymentDeletedEvent {
  type: 'payment_deleted';
  at: string;
  data: { uuid: string };
}

/**
 * Les paiements ont été purgés. `provider` est vide sur une purge
 * globale, renseigné sur une purge ciblée.
 */
export interface PaymentsPurgedEvent {
  type: 'payments_purged';
  at: string;
  data: { provider: string; deleted: number };
}

/**
 * Un webhook entre en file. Émis avant toute tentative, ce qui permet
 * de l'afficher en attente plutôt que de le voir surgir une fois
 * livré. Charge utile allégée : corps et en-têtes se récupèrent au
 * besoin par GET /webhooks/{id}.
 */
export interface WebhookEnqueuedEvent {
  type: 'webhook_enqueued';
  at: string;
  data: { id: string; url: string; attempts: number; createdAt: string };
}

/**
 * Un webhook a été remis. `data` reste unknown : c'est le
 * WebhookRecord Go complet, dont le front ne consomme rien
 * directement — il recharge la liste plutôt que de reconstruire une
 * entrée à partir de l'événement.
 */
export interface WebhookDeliveredEvent {
  type: 'webhook_delivered';
  at: string;
  data: unknown;
}

/**
 * Une livraison a échoué. Même charge utile que WebhookDeliveredEvent :
 * l'échec porte sur l'acheminement, pas sur le contenu annoncé.
 */
export interface WebhookFailedEvent {
  type: 'webhook_failed';
  at: string;
  data: unknown;
}

/**
 * Le simulateur a été remis à zéro. `data` détaille ce qui a disparu,
 * collection par collection. Toutes les vues sont concernées, d'où un
 * seul événement plutôt qu'un par table.
 */
export interface ResetEvent {
  type: 'reset';
  at: string;
  data: {
    payments: number;
    subscriptions: number;
    paymentMethods: number;
    webhooks: number;
  };
}

export type PaysimEvent =
  | PaymentCreatedEvent
  | PaymentStateChangedEvent
  | PaymentDeletedEvent
  | PaymentsPurgedEvent
  | WebhookEnqueuedEvent
  | WebhookDeliveredEvent
  | WebhookFailedEvent
  | ResetEvent;

const KNOWN_TYPES: readonly PaysimEventType[] = [
  'payment_created',
  'payment_state_changed',
  'payment_deleted',
  'payments_purged',
  'webhook_enqueued',
  'webhook_delivered',
  'webhook_failed',
  'reset',
];

/**
 * isPaysimEvent narrow un event SSE générique en PaysimEvent connu.
 * Un event de type inconnu (introduit côté serveur mais pas encore
 * côté front) est rejeté ici plutôt que de propager du unknown non
 * typé plus loin.
 */
export function isPaysimEvent(evt: { type: string }): evt is PaysimEvent {
  return (KNOWN_TYPES as readonly string[]).includes(evt.type);
}
