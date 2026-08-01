// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { isPaysimEvent } from '@/shared/model/events';

describe('isPaysimEvent', () => {
  it.each([
    'payment_created',
    'payment_state_changed',
    'webhook_enqueued',
    'webhook_delivered',
    'webhook_failed',
  ])('accepte %s', (t) => {
    expect(isPaysimEvent({ type: t })).toBe(true);
  });

  it.each(['unknown', '', 'PAYMENT_CREATED', 'payment.created'])(
    'rejette %j',
    (t) => {
      expect(isPaysimEvent({ type: t })).toBe(false);
    },
  );
});
