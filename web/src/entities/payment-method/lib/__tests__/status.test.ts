// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { isExpired, paymentMethodStatus } from '@/entities/payment-method/lib/status';

const now = new Date(2026, 7, 15); // 15 août 2026 (Date month = 0-indexé, 7 = août)

describe('isExpired', () => {
  it('année future → non expirée', () => {
    expect(isExpired({ expiryMonth: 1, expiryYear: 2028 }, now)).toBe(false);
  });

  it('année passée → expirée', () => {
    expect(isExpired({ expiryMonth: 12, expiryYear: 2020 }, now)).toBe(true);
  });

  it('même année mois futur → non expirée', () => {
    expect(isExpired({ expiryMonth: 12, expiryYear: 2026 }, now)).toBe(false);
  });

  it('même année mois passé → expirée', () => {
    expect(isExpired({ expiryMonth: 5, expiryYear: 2026 }, now)).toBe(true);
  });

  it('même année même mois → non expirée (encore valide ce mois-ci)', () => {
    expect(isExpired({ expiryMonth: 8, expiryYear: 2026 }, now)).toBe(false);
  });
});

describe('paymentMethodStatus', () => {
  const base = { expiryMonth: 12, expiryYear: 2028 };

  it('non révoquée + non expirée → active', () => {
    expect(paymentMethodStatus({ ...base, revoked: false }, now)).toBe('active');
  });

  it('révoquée + non expirée → revoked', () => {
    expect(paymentMethodStatus({ ...base, revoked: true }, now)).toBe('revoked');
  });

  it('non révoquée + expirée → expired', () => {
    expect(
      paymentMethodStatus({ revoked: false, expiryMonth: 1, expiryYear: 2020 }, now),
    ).toBe('expired');
  });

  it('révoquée + expirée → revoked (priorité révocation)', () => {
    expect(
      paymentMethodStatus({ revoked: true, expiryMonth: 1, expiryYear: 2020 }, now),
    ).toBe('revoked');
  });
});
