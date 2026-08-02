// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { PaymentMethodOutput } from '@/shared/model';

/**
 * État visuel d'un moyen de paiement — priorité :
 *   1. `revoked`  → mise hors service explicite (irrévocable).
 *   2. `expired`  → date d'expiration passée (calculée à la volée
 *                   côté UI, pas persistée serveur).
 *   3. `active`   → utilisable pour un charge_token ou trigger_billing.
 *
 * Un rejeu sur une CB expirée ou révoquée échouera côté serveur ; on
 * signale cela dans l'UI pour éviter la confusion (« pourquoi le
 * rejeu de cette CB Actif refuse ? »).
 */
export type MethodStatus = 'active' | 'expired' | 'revoked';

/**
 * isExpired : mois/année d'expiration strictement antérieurs à
 * maintenant. Convention bancaire française — une carte qui expire
 * « ce mois-ci » est encore valide jusqu'à la fin du mois. Cohérent
 * avec `PaymentMethod.IsExpired` côté serveur.
 */
export function isExpired(
  m: Pick<PaymentMethodOutput, 'expiryMonth' | 'expiryYear'>,
  now: Date = new Date(),
): boolean {
  const year = now.getFullYear();
  const month = now.getMonth() + 1;
  if (m.expiryYear < year) return true;
  if (m.expiryYear === year && m.expiryMonth < month) return true;
  return false;
}

/**
 * paymentMethodStatus retourne le state visuel effectif — révocation
 * gagne sur expiration (une CB peut être révoquée ET expirée, le
 * signal utile est « ne l'utilise pas parce qu'on l'a coupée », pas
 * « ne l'utilise pas parce que le PSP la refusera »).
 */
export function paymentMethodStatus(
  m: Pick<PaymentMethodOutput, 'revoked' | 'expiryMonth' | 'expiryYear'>,
  now: Date = new Date(),
): MethodStatus {
  if (m.revoked) return 'revoked';
  if (isExpired(m, now)) return 'expired';
  return 'active';
}
