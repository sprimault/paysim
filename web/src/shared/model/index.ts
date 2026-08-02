// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Point d'entrée du modèle API pour le front. Re-exporte les types
 * générés par tygo (api.ts) en les affinant :
 *
 * - Corrige l'embed Go anonyme (PaymentDetail/WebhookDetail) que tygo
 *   ne sait pas aplatir : côté JSON le server sérialise à plat, on
 *   modélise ça avec des types intersect.
 * - Substitue les `state`, `kind`, `status`, `outcome` bruts (string)
 *   par leurs unions fermées définies dans enums.ts.
 * - Cache les structs internes (Deps, Handler) qui ne sont pas des
 *   DTOs — le front n'a rien à en faire.
 */

import type {
  PaymentSummary as PaymentSummaryRaw,
  EventEntry as EventEntryRaw,
  WebhookEntry as WebhookEntryRaw,
  SimulatePaymentRequest as SimulatePaymentRequestRaw,
  SimulatePaymentResponse as SimulatePaymentResponseRaw,
  ReplayWebhookResponse,
  SubscriptionOutput,
  CreateSubscriptionInput,
  TriggerBillingOutput,
  PaymentMethodOutput,
} from './api';
import type {
  PaymentState,
  EventKind,
  WebhookStatus,
  PaymentOutcome,
  SimulateChannel,
} from './enums';

export type PaymentSummary = Omit<PaymentSummaryRaw, 'state'> & {
  state: PaymentState;
};

export type EventEntry = Omit<EventEntryRaw, 'kind'> & {
  kind: EventKind;
};

export type PaymentDetail = PaymentSummary & {
  events: EventEntry[];
};

export type WebhookEntry = Omit<WebhookEntryRaw, 'status'> & {
  status: WebhookStatus;
};

export type WebhookDetail = WebhookEntry & {
  headers: Record<string, string>;
  body: string;
};

export type SimulatePaymentRequest = Omit<SimulatePaymentRequestRaw, 'outcome' | 'channel'> & {
  outcome: PaymentOutcome;
  channel?: SimulateChannel;
};

export type SimulatePaymentResponse = Omit<SimulatePaymentResponseRaw, 'channel'> & {
  channel: SimulateChannel;
};

export type { ReplayWebhookResponse };
// Types 4.4.5/6/7 — subscriptions & payment methods. Réexports directs :
// pas d'affinage d'unions à faire côté front (les booléens et strings
// libres restent tels quels).
export type {
  SubscriptionOutput,
  CreateSubscriptionInput,
  TriggerBillingOutput,
  PaymentMethodOutput,
};
export type {
  PaymentState,
  EventKind,
  WebhookStatus,
  PaymentOutcome,
  SimulateChannel,
} from './enums';
export { isTerminal } from './enums';
