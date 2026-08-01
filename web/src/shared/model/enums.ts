// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Unions typées pour les champs `string` de l'API dont le domaine
 * de valeurs est fermé. Miroir des constantes Go :
 *   - PaymentState  → internal/domain/state.go
 *   - EventKind     → internal/domain/event.go
 *   - WebhookStatus → internal/delivery/queue.go
 *   - PaymentOutcome→ internal/providers/payzen (choix de simulation)
 *
 * tygo ne peut pas les déduire (Go les modélise en `type State string`
 * + constantes), donc on les redéclare ici. Si un jour une constante
 * bouge côté Go, TypeScript refusera l'usage — filet de sécurité.
 */

export type PaymentState =
  | 'initiated'
  | 'authorized'
  | 'captured'
  | 'refunded'
  | 'partially_refunded'
  | 'declined'
  | 'expired'
  | 'chargeback';

export type EventKind =
  | 'created'
  | 'authorized'
  | 'captured'
  | 'refunded'
  | 'declined'
  | 'expired'
  | 'chargeback';

export type WebhookStatus = 'pending' | 'delivered' | 'failed';

export type PaymentOutcome = 'PAID' | 'AUTHORISED' | 'UNPAID' | 'EXPIRED' | 'ABANDONED';

export type SimulateChannel = 'browserReturn' | 'ipn';

// isTerminal reproduit la logique de State.IsTerminal() côté Go —
// utile pour désactiver les actions dans l'UI une fois le paiement figé.
export function isTerminal(s: PaymentState): boolean {
  return s === 'refunded' || s === 'declined' || s === 'expired' || s === 'chargeback';
}
