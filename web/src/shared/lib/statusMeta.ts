// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { LucideIcon } from 'lucide-react';
import {
  Ban,
  CheckCircle2,
  Clock,
  CreditCard,
  Hourglass,
  RotateCcw,
  Undo2,
  XCircle,
} from 'lucide-react';
import type { MessageKey } from '@/shared/i18n/messages';
import type { EventKind, PaymentState, WebhookStatus } from '@/shared/model/enums';

// Tables uniques status/kind → (clé i18n, tone Badge, icône). Le label
// est résolu au niveau du composant via `useT(meta.labelKey)` — les
// meta restent framework-agnostic. Toute nouvelle valeur ajoutée côté
// Go doit être répercutée ici, sans quoi le switch exhaustif de
// TypeScript le signalera au build.

type BadgeTone =
  | 'paid'
  | 'unpaid'
  | 'authorised'
  | 'expired'
  | 'chargeback'
  | 'abandoned'
  | 'neutral';

interface StateMeta {
  labelKey: MessageKey;
  tone: BadgeTone;
  icon: LucideIcon;
}

export const paymentStateMeta: Record<PaymentState, StateMeta> = {
  initiated: { labelKey: 'payment.state.initiated', tone: 'neutral', icon: Hourglass },
  authorized: { labelKey: 'payment.state.authorized', tone: 'authorised', icon: Clock },
  captured: { labelKey: 'payment.state.captured', tone: 'paid', icon: CheckCircle2 },
  refunded: { labelKey: 'payment.state.refunded', tone: 'abandoned', icon: Undo2 },
  partially_refunded: { labelKey: 'payment.state.partiallyRefunded', tone: 'abandoned', icon: Undo2 },
  declined: { labelKey: 'payment.state.declined', tone: 'unpaid', icon: XCircle },
  expired: { labelKey: 'payment.state.expired', tone: 'expired', icon: Clock },
  chargeback: { labelKey: 'payment.state.chargeback', tone: 'chargeback', icon: Ban },
};

export const eventKindMeta: Record<EventKind, { labelKey: MessageKey; icon: LucideIcon }> = {
  created: { labelKey: 'event.kind.created', icon: CreditCard },
  authorized: { labelKey: 'event.kind.authorized', icon: Clock },
  captured: { labelKey: 'event.kind.captured', icon: CheckCircle2 },
  refunded: { labelKey: 'event.kind.refunded', icon: Undo2 },
  declined: { labelKey: 'event.kind.declined', icon: XCircle },
  expired: { labelKey: 'event.kind.expired', icon: Clock },
  chargeback: { labelKey: 'event.kind.chargeback', icon: Ban },
};

interface WebhookStatusMeta {
  labelKey: MessageKey;
  tone: BadgeTone;
  icon: LucideIcon;
}

export const webhookStatusMeta: Record<WebhookStatus, WebhookStatusMeta> = {
  pending: { labelKey: 'webhook.status.pending', tone: 'authorised', icon: RotateCcw },
  delivered: { labelKey: 'webhook.status.delivered', tone: 'paid', icon: CheckCircle2 },
  failed: { labelKey: 'webhook.status.failed', tone: 'unpaid', icon: XCircle },
};
