// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { isTerminal, type PaymentState } from '@/shared/model/enums';

describe('isTerminal', () => {
  it.each<[PaymentState, boolean]>([
    ['initiated', false],
    ['authorized', false],
    ['captured', false],
    ['partially_refunded', false],
    ['refunded', true],
    ['declined', true],
    ['expired', true],
    ['chargeback', true],
  ])('isTerminal(%s) = %s', (state, expected) => {
    expect(isTerminal(state)).toBe(expected);
  });
});
