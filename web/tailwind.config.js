// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono Variable', 'ui-monospace', 'monospace'],
      },
      colors: {
        // Palette Paysim — accent indigo (moderne, tech, cohérent avec
        // l'esthétique d'un outil dev pro). Statuts alignés sur les
        // conventions PSP : green = paid, red = unpaid, blue =
        // authorised (fonds réservés), amber = chargeback/expired.
        brand: {
          50: '#eef2ff',
          100: '#e0e7ff',
          200: '#c7d2fe',
          300: '#a5b4fc',
          400: '#818cf8',
          500: '#6366f1',
          600: '#4f46e5',
          700: '#4338ca',
          800: '#3730a3',
          900: '#312e81',
          950: '#1e1b4b',
        },
        status: {
          paid: '#10b981', // emerald-500
          unpaid: '#f43f5e', // rose-500
          authorised: '#0ea5e9', // sky-500
          expired: '#71717a', // zinc-500
          chargeback: '#f59e0b', // amber-500
          abandoned: '#a1a1aa', // zinc-400
        },
      },
      borderRadius: {
        panel: '0.75rem',
      },
      boxShadow: {
        // Ombres subtiles, cohérence design system.
        card: '0 1px 3px rgba(15, 23, 42, 0.08), 0 1px 2px rgba(15, 23, 42, 0.04)',
        'card-hover': '0 4px 12px rgba(15, 23, 42, 0.10), 0 2px 4px rgba(15, 23, 42, 0.06)',
        panel: '0 10px 40px rgba(15, 23, 42, 0.10), 0 4px 12px rgba(15, 23, 42, 0.06)',
      },
      animation: {
        'fade-in': 'fadeIn 150ms ease-out',
        'slide-in-right': 'slideInRight 200ms ease-out',
        'pulse-slow': 'pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(8px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' },
        },
      },
    },
  },
  plugins: [],
};
