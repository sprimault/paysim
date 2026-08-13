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

  // Le chemin passé est relatif à la racine de l'API : c'est apiUrl qui
  // porte /paysim/api/v1, pour que l'oublier soit impossible.
  it('préfixe le chemin par la racine d’API et le base path', () => {
    window.__PAYSIM_BASE_PATH__ = '/sous-chemin';
    expect(apiUrl('/payments')).toBe('/sous-chemin/paysim/api/v1/payments');
  });

  it('préfixe par la seule racine d’API quand base path absent', () => {
    delete window.__PAYSIM_BASE_PATH__;
    expect(apiUrl('/payments')).toBe('/paysim/api/v1/payments');
  });
});
