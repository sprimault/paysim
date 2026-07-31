// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

/**
 * Configuration Vitest — tests unitaires frontend.
 *
 * Environnement jsdom : simule le DOM du navigateur.
 * Setup file : charge @testing-library/jest-dom pour les matchers
 * supplémentaires (toBeInTheDocument, toHaveTextContent, etc.).
 *
 * Convention : les fichiers de test vivent à côté du code sous le
 * suffixe .test.ts / .test.tsx.
 */
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    exclude: ['node_modules', 'dist'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/main.tsx',
        'src/app/App.tsx',
        'src/**/__tests__/**',
        'src/test/**',
      ],
    },
  },
});
