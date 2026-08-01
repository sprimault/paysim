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
 */

// Discriminant complet des events publiés par Paysim v1.
export type PaysimEventType =
  | 'payment_created'
  | 'payment_state_changed'
  | 'payment_deleted'
  | 'payments_purged'
  | 'webhook_enqueued'
  | 'webhook_delivered'
  | 'webhook_failed';

export interface PaymentCreatedEvent {
  type: 'payment_created';
  at: string;
  data: { uuid: string; orderId: string; amount: number; currency: string };
}

export interface PaymentStateChangedEvent {
  type: 'payment_state_changed';
  at: string;
  data: { uuid: string; orderId: string; state: string; outcome: string };
}

export interface PaymentDeletedEvent {
  type: 'payment_deleted';
  at: string;
  data: { uuid: string };
}

export interface PaymentsPurgedEvent {
  type: 'payments_purged';
  at: string;
  data: { provider: string; deleted: number };
}

export interface WebhookEnqueuedEvent {
  type: 'webhook_enqueued';
  at: string;
  data: { id: string; url: string; attempts: number; createdAt: string };
}

export interface WebhookDeliveredEvent {
  type: 'webhook_delivered';
  at: string;
  data: unknown; // WebhookRecord Go — champs Webhook + Status + StatusCode + ErrorMsg + CompletedAt
}

export interface WebhookFailedEvent {
  type: 'webhook_failed';
  at: string;
  data: unknown; // Idem WebhookDeliveredEvent
}

export type PaysimEvent =
  | PaymentCreatedEvent
  | PaymentStateChangedEvent
  | PaymentDeletedEvent
  | PaymentsPurgedEvent
  | WebhookEnqueuedEvent
  | WebhookDeliveredEvent
  | WebhookFailedEvent;

const KNOWN_TYPES: readonly PaysimEventType[] = [
  'payment_created',
  'payment_state_changed',
  'payment_deleted',
  'payments_purged',
  'webhook_enqueued',
  'webhook_delivered',
  'webhook_failed',
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
