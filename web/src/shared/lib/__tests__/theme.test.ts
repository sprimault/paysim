// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  applyToDocument,
  getStoredTheme,
  resolveEffective,
  setStoredTheme,
  STORAGE_KEY,
} from '@/shared/lib/theme';

describe('theme', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove('dark');
  });

  describe('getStoredTheme', () => {
    it('retourne system par défaut', () => {
      expect(getStoredTheme()).toBe('system');
    });

    it('retourne la valeur enregistrée si valide', () => {
      localStorage.setItem(STORAGE_KEY, 'dark');
      expect(getStoredTheme()).toBe('dark');
      localStorage.setItem(STORAGE_KEY, 'light');
      expect(getStoredTheme()).toBe('light');
    });

    it('retourne system si valeur invalide enregistrée', () => {
      localStorage.setItem(STORAGE_KEY, 'ultra-violet');
      expect(getStoredTheme()).toBe('system');
    });
  });

  describe('setStoredTheme', () => {
    it('persiste dans localStorage', () => {
      setStoredTheme('dark');
      expect(localStorage.getItem(STORAGE_KEY)).toBe('dark');
    });

    it('écrase la valeur précédente', () => {
      setStoredTheme('light');
      setStoredTheme('dark');
      expect(localStorage.getItem(STORAGE_KEY)).toBe('dark');
    });
  });

  describe('resolveEffective', () => {
    afterEach(() => {
      vi.restoreAllMocks();
      vi.unstubAllGlobals();
    });

    it('retourne light tel quel', () => {
      expect(resolveEffective('light')).toBe('light');
    });

    it('retourne dark tel quel', () => {
      expect(resolveEffective('dark')).toBe('dark');
    });

    it('consulte prefers-color-scheme pour system → dark', () => {
      // jsdom expose matchMedia mais il n'est pas spyable directement
      // (configurable: false). On l'écrase via stubGlobal.
      vi.stubGlobal(
        'matchMedia',
        () =>
          ({
            matches: true,
            media: '(prefers-color-scheme: dark)',
            addEventListener: () => {},
            removeEventListener: () => {},
          }) as unknown as MediaQueryList,
      );
      expect(resolveEffective('system')).toBe('dark');
    });

    it('consulte prefers-color-scheme pour system → light', () => {
      vi.stubGlobal(
        'matchMedia',
        () =>
          ({
            matches: false,
            media: '(prefers-color-scheme: dark)',
            addEventListener: () => {},
            removeEventListener: () => {},
          }) as unknown as MediaQueryList,
      );
      expect(resolveEffective('system')).toBe('light');
    });
  });

  describe('applyToDocument', () => {
    it('ajoute la classe dark sur <html> pour dark', () => {
      applyToDocument('dark');
      expect(document.documentElement.classList.contains('dark')).toBe(true);
    });

    it('retire la classe dark pour light', () => {
      document.documentElement.classList.add('dark');
      applyToDocument('light');
      expect(document.documentElement.classList.contains('dark')).toBe(false);
    });

    it('est idempotent (double appel dark)', () => {
      applyToDocument('dark');
      applyToDocument('dark');
      expect(document.documentElement.classList.contains('dark')).toBe(true);
    });
  });
});
