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
import type { EventKind, PaymentState, WebhookStatus } from '@/shared/model/enums';

// Tables uniques status/kind → (libellé français, tone Badge, icône). Toute
// nouvelle valeur ajoutée côté Go doit être répercutée ici — sans quoi le
// switch exhaustif de TypeScript le signalera au build.

type BadgeTone =
  | 'paid'
  | 'unpaid'
  | 'authorised'
  | 'expired'
  | 'chargeback'
  | 'abandoned'
  | 'neutral';

interface StateMeta {
  label: string;
  tone: BadgeTone;
  icon: LucideIcon;
}

export const paymentStateMeta: Record<PaymentState, StateMeta> = {
  initiated: { label: 'Initié', tone: 'neutral', icon: Hourglass },
  authorized: { label: 'Autorisé', tone: 'authorised', icon: Clock },
  captured: { label: 'Payé', tone: 'paid', icon: CheckCircle2 },
  refunded: { label: 'Remboursé', tone: 'abandoned', icon: Undo2 },
  partially_refunded: { label: 'Rembours. partiel', tone: 'abandoned', icon: Undo2 },
  declined: { label: 'Refusé', tone: 'unpaid', icon: XCircle },
  expired: { label: 'Expiré', tone: 'expired', icon: Clock },
  chargeback: { label: 'Rétrofacturation', tone: 'chargeback', icon: Ban },
};

export const eventKindMeta: Record<EventKind, { label: string; icon: LucideIcon }> = {
  created: { label: 'Créé', icon: CreditCard },
  authorized: { label: 'Autorisé', icon: Clock },
  captured: { label: 'Capturé', icon: CheckCircle2 },
  refunded: { label: 'Remboursé', icon: Undo2 },
  declined: { label: 'Refusé', icon: XCircle },
  expired: { label: 'Expiré', icon: Clock },
  chargeback: { label: 'Rétrofacturation', icon: Ban },
};

interface WebhookStatusMeta {
  label: string;
  tone: BadgeTone;
  icon: LucideIcon;
}

export const webhookStatusMeta: Record<WebhookStatus, WebhookStatusMeta> = {
  pending: { label: 'En attente', tone: 'authorised', icon: RotateCcw },
  delivered: { label: 'Livré', tone: 'paid', icon: CheckCircle2 },
  failed: { label: 'Échec', tone: 'unpaid', icon: XCircle },
};
