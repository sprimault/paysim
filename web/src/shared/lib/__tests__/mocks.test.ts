// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { mockPaymentDetail, mockPayments, mockWebhooks } from '../mocks';

describe('mocks', () => {
  it('mockPayments couvre les statuts principaux', () => {
    const states = new Set(mockPayments.map((p) => p.state));
    expect(states.has('captured')).toBe(true);
    expect(states.has('authorized')).toBe(true);
    expect(states.has('declined')).toBe(true);
    expect(states.has('partially_refunded')).toBe(true);
    expect(states.has('expired')).toBe(true);
    expect(states.has('chargeback')).toBe(true);
  });

  it('chaque paiement a un uuid unique', () => {
    const uuids = mockPayments.map((p) => p.uuid);
    expect(new Set(uuids).size).toBe(uuids.length);
  });

  it('mockPaymentDetail renvoie undefined pour un uuid inconnu', () => {
    expect(mockPaymentDetail('inconnu')).toBeUndefined();
  });

  it('mockPaymentDetail joint des événements cohérents pour un paiement connu', () => {
    const p = mockPayments[0];
    const d = mockPaymentDetail(p.uuid);
    expect(d).toBeDefined();
    expect(d?.uuid).toBe(p.uuid);
    expect(d?.events.length).toBeGreaterThan(0);
    expect(d?.events[0].kind).toBe('created');
  });

  it('mockWebhooks contient au moins un livré et un échec', () => {
    const statuses = mockWebhooks.map((w) => w.status);
    expect(statuses).toContain('delivered');
    expect(statuses).toContain('failed');
  });
});
