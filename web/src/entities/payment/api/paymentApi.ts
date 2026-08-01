// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiGetJson, apiPostJson } from '@/shared/api/client';
import type {
  PaymentDetail,
  PaymentSummary,
  SimulatePaymentRequest,
  SimulatePaymentResponse,
} from '@/shared/model';

/**
 * Client REST pour l'entité Payment. Wrappers typés au-dessus des
 * endpoints /paysim/api/v1/payments/*. Aucun state local — c'est
 * paymentStore qui garde la donnée.
 */

const BASE = '/paysim/api/v1/payments';

export function fetchPayments(signal?: AbortSignal): Promise<PaymentSummary[]> {
  return apiGetJson<PaymentSummary[]>(BASE, signal);
}

export function fetchPayment(uuid: string, signal?: AbortSignal): Promise<PaymentDetail> {
  return apiGetJson<PaymentDetail>(`${BASE}/${encodeURIComponent(uuid)}`, signal);
}

export function simulatePayment(
  uuid: string,
  req: SimulatePaymentRequest,
  signal?: AbortSignal,
): Promise<SimulatePaymentResponse> {
  return apiPostJson<SimulatePaymentRequest, SimulatePaymentResponse>(
    `${BASE}/${encodeURIComponent(uuid)}/simulate`,
    req,
    signal,
  );
}
