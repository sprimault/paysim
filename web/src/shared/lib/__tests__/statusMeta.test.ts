// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { eventKindMeta, paymentStateMeta, webhookStatusMeta } from '@/shared/lib/statusMeta';
import type { EventKind, PaymentState, WebhookStatus } from '@/shared/model/enums';

// Ces tests garantissent que chaque valeur d'enum a bien une entrée
// dans les tables — filet de sécurité si on ajoute une valeur côté Go
// et qu'on oublie de la déclarer côté UI.

describe('paymentStateMeta', () => {
  const states: PaymentState[] = [
    'initiated',
    'authorized',
    'captured',
    'refunded',
    'partially_refunded',
    'declined',
    'expired',
    'chargeback',
  ];

  it.each(states)('a une entrée complète pour %s', (s) => {
    const m = paymentStateMeta[s];
    expect(m).toBeDefined();
    expect(m.label.length).toBeGreaterThan(0);
    expect(m.tone).toBeDefined();
    expect(m.icon).toBeDefined();
  });
});

describe('eventKindMeta', () => {
  const kinds: EventKind[] = [
    'created',
    'authorized',
    'captured',
    'refunded',
    'declined',
    'expired',
    'chargeback',
  ];

  it.each(kinds)('a une entrée complète pour %s', (k) => {
    const m = eventKindMeta[k];
    expect(m).toBeDefined();
    expect(m.label.length).toBeGreaterThan(0);
    expect(m.icon).toBeDefined();
  });
});

describe('webhookStatusMeta', () => {
  const statuses: WebhookStatus[] = ['pending', 'delivered', 'failed'];

  it.each(statuses)('a une entrée complète pour %s', (s) => {
    const m = webhookStatusMeta[s];
    expect(m).toBeDefined();
    expect(m.label.length).toBeGreaterThan(0);
    expect(m.tone).toBeDefined();
    expect(m.icon).toBeDefined();
  });
});
