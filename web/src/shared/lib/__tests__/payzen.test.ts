// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { parsePayzenBody } from '../payzen';

describe('parsePayzenBody', () => {
  it('extrait kr-answer parsé et sépare les autres champs', () => {
    const body =
      'kr-hash=a1b2c3&kr-hash-algorithm=sha256_hmac&kr-answer-type=V4%2FPayment' +
      '&kr-answer=%7B%22orderStatus%22%3A%22PAID%22%2C%22uuid%22%3A%22abc%22%7D';
    const out = parsePayzenBody(body);
    expect(out.krAnswer).toEqual({ orderStatus: 'PAID', uuid: 'abc' });
    expect(out.rest).toEqual({
      'kr-hash': 'a1b2c3',
      'kr-hash-algorithm': 'sha256_hmac',
      'kr-answer-type': 'V4/Payment',
    });
  });

  it('renvoie kr-answer en string quand le JSON est invalide', () => {
    const body = 'kr-answer=not-json&kr-hash=abc';
    const out = parsePayzenBody(body);
    expect(out.krAnswer).toBe('not-json');
    expect(out.rest).toEqual({ 'kr-hash': 'abc' });
  });

  it('renvoie krAnswer null quand le champ est absent', () => {
    const body = 'kr-hash=abc&kr-answer-type=V4%2FPayment';
    const out = parsePayzenBody(body);
    expect(out.krAnswer).toBeNull();
    expect(out.rest['kr-hash']).toBe('abc');
    expect(out.rest['kr-answer-type']).toBe('V4/Payment');
  });

  it('gère un body vide', () => {
    const out = parsePayzenBody('');
    expect(out.krAnswer).toBeNull();
    expect(out.rest).toEqual({});
  });

  it('décode les caractères spéciaux url-encoded', () => {
    const body = 'kr-answer=%7B%22msg%22%3A%22h%C3%A9llo%22%7D';
    const out = parsePayzenBody(body);
    expect(out.krAnswer).toEqual({ msg: 'héllo' });
  });
});
