// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiDelete, apiGetJson, apiPostJson } from '@/shared/api/client';
import type {
  DeletePaymentOutput,
  PaymentDetail,
  PaymentSummary,
  PurgePaymentsOutput,
  SimulatePaymentRequest,
  SimulatePaymentResponse,
} from '@/shared/model';

/**
 * Client REST pour l'entité Payment. Wrappers typés au-dessus des
 * endpoints /paysim/api/v1/payments/*. Aucun state local — c'est
 * paymentStore qui garde la donnée.
 */

const BASE = '/payments';

/** Tous les paiements, plus récemment modifié d'abord. */
export function fetchPayments(signal?: AbortSignal): Promise<PaymentSummary[]> {
  return apiGetJson<PaymentSummary[]>(BASE, signal);
}

/**
 * Échéances produites par un abonnement, réussies comme refusées.
 *
 * Le filtre est côté serveur : le rattachement vit dans les métadonnées
 * du paiement, que le sommaire n'expose pas. Un tri local serait donc
 * impossible sans transporter toutes les métadonnées de toute la liste.
 */
export function fetchPaymentsBySubscription(
  subscriptionId: string,
  signal?: AbortSignal,
): Promise<PaymentSummary[]> {
  return apiGetJson<PaymentSummary[]>(
    `${BASE}?subscriptionId=${encodeURIComponent(subscriptionId)}`,
    signal,
  );
}

/**
 * Paiements liés à un moyen enregistré : celui qui l'a enrôlé comme ceux
 * qui l'ont débité. Alimente le bloc « Utilisations » de la fiche du
 * moyen — la lecture inverse de PaymentSummary.paymentMethodToken.
 */
export function fetchPaymentsByToken(
  token: string,
  signal?: AbortSignal,
): Promise<PaymentSummary[]> {
  return apiGetJson<PaymentSummary[]>(
    `${BASE}?paymentMethodToken=${encodeURIComponent(token)}`,
    signal,
  );
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

/**
 * deletePayment supprime un paiement et ses livraisons. Idempotent —
 * un UUID inconnu renvoie 204 sans erreur.
 *
 * Le corps n'existe que si la cascade a échoué : le cas nominal répond
 * 204, d'où l'`undefined` du type de retour.
 */
export function deletePayment(
  uuid: string,
  signal?: AbortSignal,
): Promise<DeletePaymentOutput | undefined> {
  return apiDelete<DeletePaymentOutput | undefined>(
    `${BASE}/${encodeURIComponent(uuid)}`,
    signal,
  );
}

/**
 * purgePayments supprime tous les paiements, avec filtre provider
 * optionnel. Sans `provider`, purge cross-provider complète.
 *
 * La forme de la réponse est générée depuis le DTO Go : elle a gagné
 * le compte des livraisons emportées, et une réécriture à la main
 * l'aurait manqué.
 */
export function purgePayments(
  provider?: string,
  signal?: AbortSignal,
): Promise<PurgePaymentsOutput> {
  const path = provider
    ? `${BASE}?provider=${encodeURIComponent(provider)}`
    : BASE;
  return apiDelete<PurgePaymentsOutput>(path, signal);
}
