// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { apiUrl, getBasePath } from '@/shared/api/basePath';

describe('getBasePath', () => {
  const original = window.__PAYSIM_BASE_PATH__;

  beforeEach(() => {
    delete window.__PAYSIM_BASE_PATH__;
  });

  afterEach(() => {
    if (original === undefined) {
      delete window.__PAYSIM_BASE_PATH__;
    } else {
      window.__PAYSIM_BASE_PATH__ = original;
    }
  });

  it('retourne "" quand window.__PAYSIM_BASE_PATH__ absent', () => {
    expect(getBasePath()).toBe('');
  });

  it('retourne la valeur injectée par le backend', () => {
    window.__PAYSIM_BASE_PATH__ = '/paysim';
    expect(getBasePath()).toBe('/paysim');
  });

  it('respecte la chaîne vide comme valeur explicite', () => {
    window.__PAYSIM_BASE_PATH__ = '';
    expect(getBasePath()).toBe('');
  });
});

describe('apiUrl', () => {
  const original = window.__PAYSIM_BASE_PATH__;

  afterEach(() => {
    if (original === undefined) {
      delete window.__PAYSIM_BASE_PATH__;
    } else {
      window.__PAYSIM_BASE_PATH__ = original;
    }
  });

  it('préfixe le chemin par le base path', () => {
    window.__PAYSIM_BASE_PATH__ = '/paysim';
    expect(apiUrl('/api/v1/payments')).toBe('/paysim/api/v1/payments');
  });

  it('renvoie le chemin nu quand base path absent', () => {
    delete window.__PAYSIM_BASE_PATH__;
    expect(apiUrl('/api/v1/payments')).toBe('/api/v1/payments');
  });
});
