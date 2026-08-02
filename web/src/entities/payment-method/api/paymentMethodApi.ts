// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiGetJson, apiPostJson } from '@/shared/api/client';
import type { PaymentMethodOutput } from '@/shared/model';

/**
 * Client REST pour l'entité PaymentMethod. Wrappers typés au-dessus
 * de /paysim/api/v1/payment-methods/*. Aucun state local — c'est
 * paymentMethodStore qui garde la donnée.
 */

const BASE = '/paysim/api/v1/payment-methods';

export function fetchPaymentMethods(signal?: AbortSignal): Promise<PaymentMethodOutput[]> {
  return apiGetJson<PaymentMethodOutput[]>(BASE, signal);
}

export function fetchPaymentMethod(
  token: string,
  signal?: AbortSignal,
): Promise<PaymentMethodOutput> {
  return apiGetJson<PaymentMethodOutput>(
    `${BASE}/${encodeURIComponent(token)}`,
    signal,
  );
}

/**
 * revokePaymentMethod marque le moyen comme révoqué. Idempotent —
 * un token inconnu renvoie 204 sans erreur.
 */
export function revokePaymentMethod(token: string, signal?: AbortSignal): Promise<void> {
  return apiPostJson<Record<string, never>, void>(
    `${BASE}/${encodeURIComponent(token)}/revoke`,
    {},
    signal,
  );
}
