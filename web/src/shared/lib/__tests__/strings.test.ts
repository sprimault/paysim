// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { truncate, mask, matchesSearch } from '@/shared/lib/strings';

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

describe('matchesSearch', () => {
  it('une recherche vide laisse tout passer', () => {
    expect(matchesSearch('', 'quoi que ce soit')).toBe(true);
    expect(matchesSearch('   ', undefined)).toBe(true);
  });

  it('correspondance partielle et insensible a la casse', () => {
    expect(matchesSearch('cmd-10', 'CMD-1042')).toBe(true);
    expect(matchesSearch('CMD', 'cmd-1042')).toBe(true);
    expect(matchesSearch('1043', 'CMD-1042')).toBe(false);
  });

  it('cherche dans tous les champs fournis', () => {
    expect(matchesSearch('bbb', 'CMD-1', 'aaa-111', 'bbb-222')).toBe(true);
  });

  // Un champ absent n'est pas une chaine vide : un paiement sans alias
  // ne doit pas ressortir sur une recherche qui ne le concerne pas.
  it('ignore les champs absents', () => {
    expect(matchesSearch('x', undefined, undefined)).toBe(false);
  });
});
