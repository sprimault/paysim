// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiGetJson, apiPostJson } from '@/shared/api/client';
import type {
  CreateSubscriptionInput,
  SubscriptionOutput,
  TriggerBillingOutput,
} from '@/shared/model';

/**
 * Client REST pour l'entité Subscription. Wrappers typés au-dessus
 * de /paysim/api/v1/subscriptions/*. Aucun state local — c'est
 * subscriptionStore qui garde la donnée.
 */

const BASE = '/subscriptions';

export function fetchSubscriptions(signal?: AbortSignal): Promise<SubscriptionOutput[]> {
  return apiGetJson<SubscriptionOutput[]>(BASE, signal);
}

/**
 * Abonnements prélevant un moyen donné. Un alias révoqué dont il reste
 * un abonnement actif est exactement ce qu'on veut voir d'un coup d'œil.
 */
export function fetchSubscriptionsByToken(
  token: string,
  signal?: AbortSignal,
): Promise<SubscriptionOutput[]> {
  return apiGetJson<SubscriptionOutput[]>(
    `${BASE}?paymentMethodToken=${encodeURIComponent(token)}`,
    signal,
  );
}

export function fetchSubscription(
  id: string,
  signal?: AbortSignal,
): Promise<SubscriptionOutput> {
  return apiGetJson<SubscriptionOutput>(`${BASE}/${encodeURIComponent(id)}`, signal);
}

export function createSubscription(
  req: CreateSubscriptionInput,
  signal?: AbortSignal,
): Promise<SubscriptionOutput> {
  return apiPostJson<CreateSubscriptionInput, SubscriptionOutput>(BASE, req, signal);
}

/**
 * triggerBilling déclenche une échéance manuelle et retourne l'uuid
 * du paiement créé + son état (captured ou declined).
 */
export function triggerBilling(
  id: string,
  signal?: AbortSignal,
): Promise<TriggerBillingOutput> {
  return apiPostJson<Record<string, never>, TriggerBillingOutput>(
    `${BASE}/${encodeURIComponent(id)}/trigger-billing`,
    {},
    signal,
  );
}

/**
 * cancelSubscription annule un abonnement. Idempotent — un ID
 * inconnu renvoie 204 sans erreur.
 */
export function cancelSubscription(id: string, signal?: AbortSignal): Promise<void> {
  return apiPostJson<Record<string, never>, void>(
    `${BASE}/${encodeURIComponent(id)}/cancel`,
    {},
    signal,
  );
}
