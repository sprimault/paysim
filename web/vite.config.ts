// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Configuration Vite pour l'UI Paysim.
//
// - Alias '@' vers ./src pour les imports courts.
// - Proxy /paysim/* vers le backend Go (localhost:8080) en dev — le
//   binaire Paysim tourne à côté, `npm run dev` sert le HMR sur 5173
//   et proxy les appels API au backend.
// - Bundle sortant dans ../dist non — on garde web/dist/ (relatif au
//   web/) parce que le go:embed côté serveur pointe sur web/dist/*.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/paysim': 'http://localhost:8080',
    },
  },
  // base: './' — chemins d'assets relatifs à index.html. Permet au
  // même bundle de fonctionner quel que soit le PAYSIM_BASE_PATH sous
  // lequel le binaire Go sert le SPA, sans rebuild.
  base: './',
  build: {
    // Sort le bundle dans internal/webui/dist où le paquet Go
    // //go:embed le récupère. Évite d'avoir du .go dans web/ (qui
    // héberge node_modules — go test ./... embarquerait des paquets
    // Go transitifs de npm).
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 800,
    rollupOptions: {
      output: {
        manualChunks(id) {
          // Isoler les icônes Lucide (souvent lourdes en cumul).
          if (id.includes('node_modules/lucide-react')) {
            return 'vendor-icons';
          }
          // React + router + zustand = coeur commun.
          if (id.includes('node_modules')) {
            return 'vendor-core';
          }
        },
      },
    },
  },
});
