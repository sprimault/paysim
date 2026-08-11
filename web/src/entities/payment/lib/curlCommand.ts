// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiUrl } from '@/shared/api/basePath';
import type { PaymentInStore } from '@/entities/payment/model/paymentStore';

/**
 * Construction de la commande `curl` qui rejoue un paiement.
 *
 * Rejouer, et non relire : c'est le geste du débogage — « refais-moi le
 * même cas » — là où un GET ne ferait que reproduire ce que l'écran
 * affiche déjà. La commande vise donc l'API générique de création, pas
 * la route native d'un fournisseur, qui serait à réécrire à l'arrivée
 * du deuxième.
 */

/** Corps envoyé au rejeu, dans l'ordre où on veut le relire. */
interface CorpsRejeu {
  provider: string;
  amount: number;
  currency: string;
  orderId: string;
  customer?: unknown;
  metadata?: Record<string, string>;
  paymentMethodToken?: string;
}

/**
 * buildReplayCurl rend la commande shell qui recrée ce paiement.
 *
 * Une carte n'y figure jamais : Paysim ne restitue que le PAN masqué,
 * et en inventer un produirait une commande qui marche mais ne rejoue
 * pas le même cas. Quand le paiement porte un alias, c'est lui qui est
 * repris — le rejeu one-click est alors fidèle, et c'est le seul rejeu
 * possible d'un paiement par carte.
 *
 * Sur une seule ligne, en guillemets simples : c'est ce que produisent
 * les navigateurs pour « copier en curl », et ce qui se colle sans
 * retouche dans un shell POSIX.
 */
export function buildReplayCurl(
  payment: PaymentInStore,
  origin: string = window.location.origin,
): string {
  const corps: CorpsRejeu = {
    provider: payment.provider,
    amount: payment.amount,
    currency: payment.currency,
    orderId: payment.orderId,
  };
  if (payment.customer) corps.customer = payment.customer;
  // Un objet vide se sérialise en `{}`, qui n'apporte rien et allonge
  // une commande déjà longue.
  if (payment.metadata && Object.keys(payment.metadata).length > 0) {
    corps.metadata = payment.metadata;
  }
  if (payment.paymentMethodToken) corps.paymentMethodToken = payment.paymentMethodToken;

  const url = `${origin}${apiUrl('/paysim/api/v1/payments')}`;
  return (
    `curl -X POST ${quoteShell(url)} ` +
    `-H 'Content-Type: application/json' ` +
    `-d ${quoteShell(JSON.stringify(corps))}`
  );
}

/**
 * quoteShell entoure une valeur de guillemets simples pour un shell
 * POSIX.
 *
 * Le guillemet simple est le seul caractère qu'un tel littéral ne peut
 * pas contenir : on ferme, on en insère un échappé, on rouvre. Sans
 * cela, un `orderId` comme `L'ete` couperait la commande en deux —
 * silencieusement, puisque le shell l'accepterait quand même.
 */
function quoteShell(valeur: string): string {
  return `'${valeur.replace(/'/g, `'\\''`)}'`;
}
