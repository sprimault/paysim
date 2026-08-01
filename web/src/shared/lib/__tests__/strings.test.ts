// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { truncate, mask } from '@/shared/lib/strings';

describe('truncate', () => {
  it.each([
    ['', 5, ''],
    ['abc', 5, 'abc'],
    ['abcde', 5, 'abcde'],
    ['abcdef', 5, 'abcde…'],
    ['hello world', 5, 'hello…'],
    ['héllo', 3, 'hél…'], // frontière UTF-16 respectée
    ['éàü', 2, 'éà…'],
    ['abc', 0, '…'],
    ['abc', -1, '…'],
    ['', 0, ''],
  ])('truncate(%j, %d) = %j', (input, max, expected) => {
    expect(truncate(input, max)).toBe(expected);
  });
});

describe('mask', () => {
  it.each([
    ['4111111111111111', 4, 4, '4111********1111'],
    ['abcdef1234567890abcdef', 4, 4, 'abcd**************cdef'],
    ['abc', 1, 1, '***'],
    ['abcdef', 2, 2, '***'], // 2+2+3 > 6 → opaque
    ['abcdefg', 2, 2, 'ab***fg'],
    ['', 4, 4, '***'],
    ['4111111111111111', -1, -1, '****************'],
    ['abcdefgh', 4, 0, 'abcd****'],
    ['abcdefgh', 0, 4, '****efgh'],
  ])('mask(%j, %d, %d) = %j', (input, prefix, suffix, expected) => {
    expect(mask(input, prefix, suffix)).toBe(expected);
  });
});
