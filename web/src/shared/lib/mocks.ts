// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Jeu de données factices pour développer l'UI avant que le SSE et
 * l'API réelle soient branchés (sous-vertical 3c). À supprimer d'un
 * bloc quand 3c wire le vrai client.
 */

import type { PaymentDetail, PaymentSummary, WebhookDetail } from '../model';

const now = new Date('2026-08-01T14:30:00Z').getTime();
const iso = (offsetMinutes: number) => new Date(now - offsetMinutes * 60_000).toISOString();

export const mockPayments: PaymentSummary[] = [
  {
    uuid: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
    orderId: 'CMD-2026-000123',
    amount: 4990,
    currency: 'EUR',
    state: 'captured',
    createdAt: iso(3),
    updatedAt: iso(2),
  },
  {
    uuid: 'b2c3d4e5-f6a7-8901-bcde-f12345678901',
    orderId: 'CMD-2026-000122',
    amount: 12000,
    currency: 'EUR',
    state: 'authorized',
    createdAt: iso(8),
    updatedAt: iso(7),
  },
  {
    uuid: 'c3d4e5f6-a7b8-9012-cdef-123456789012',
    orderId: 'CMD-2026-000121',
    amount: 890,
    currency: 'EUR',
    state: 'declined',
    createdAt: iso(15),
    updatedAt: iso(14),
  },
  {
    uuid: 'd4e5f6a7-b8c9-0123-def0-234567890123',
    orderId: 'CMD-2026-000120',
    amount: 25000,
    currency: 'EUR',
    state: 'partially_refunded',
    createdAt: iso(120),
    updatedAt: iso(30),
  },
  {
    uuid: 'e5f6a7b8-c9d0-1234-ef01-345678901234',
    orderId: 'CMD-2026-000119',
    amount: 5000,
    currency: 'EUR',
    state: 'expired',
    createdAt: iso(1500),
    updatedAt: iso(60),
  },
  {
    uuid: 'f6a7b8c9-d0e1-2345-f012-456789012345',
    orderId: 'CMD-2026-000118',
    amount: 8750,
    currency: 'EUR',
    state: 'chargeback',
    createdAt: iso(4000),
    updatedAt: iso(200),
  },
];

export const mockPaymentDetail = (uuid: string): PaymentDetail | undefined => {
  const s = mockPayments.find((p) => p.uuid === uuid);
  if (!s) return undefined;
  const events =
    s.state === 'captured'
      ? [
          { at: s.createdAt, kind: 'created' as const, amount: s.amount },
          { at: s.updatedAt, kind: 'captured' as const, amount: s.amount },
        ]
      : s.state === 'authorized'
        ? [
            { at: s.createdAt, kind: 'created' as const, amount: s.amount },
            { at: s.updatedAt, kind: 'authorized' as const, amount: s.amount },
          ]
        : s.state === 'declined'
          ? [
              { at: s.createdAt, kind: 'created' as const, amount: s.amount },
              {
                at: s.updatedAt,
                kind: 'declined' as const,
                amount: 0,
                note: 'Carte refusée par la banque émettrice',
              },
            ]
          : s.state === 'partially_refunded'
            ? [
                { at: s.createdAt, kind: 'created' as const, amount: s.amount },
                { at: iso(90), kind: 'captured' as const, amount: s.amount },
                { at: s.updatedAt, kind: 'refunded' as const, amount: 5000 },
              ]
            : [{ at: s.createdAt, kind: 'created' as const, amount: s.amount }];
  return { ...s, events };
};

export const mockWebhooks: WebhookDetail[] = [
  {
    id: 'wh-01H8YKM3P5N9T2Z',
    url: 'https://marchand.example/callback/payzen',
    status: 'delivered',
    statusCode: 200,
    attempts: 1,
    createdAt: iso(2),
    completedAt: iso(2),
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      'X-Paysim-Delivery': 'wh-01H8YKM3P5N9T2Z',
    },
    body: 'kr-hash=a95c2b13d50d57858ff38e7abd76c39d644fd5d1cfdcc360e4c61f2fc48d4a5e&kr-hash-algorithm=sha256_hmac&kr-hash-key=sha256_hmac&kr-answer-type=V4%2FPayment&kr-answer=%7B%22orderStatus%22%3A%22PAID%22%7D',
  },
  {
    id: 'wh-01H8YKM4Q6P0U3A',
    url: 'https://marchand.example/callback/payzen',
    status: 'failed',
    statusCode: 500,
    errorMsg: 'unexpected EOF',
    attempts: 3,
    createdAt: iso(14),
    completedAt: iso(13),
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: 'kr-hash=deadbeef&kr-answer=%7B%22orderStatus%22%3A%22UNPAID%22%7D',
  },
];
