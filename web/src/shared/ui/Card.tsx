// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';

interface CardProps {
  children: ReactNode;
  className?: string;
  padded?: boolean;
}

/**
 * Card — conteneur neutre avec bordure fine et fond légèrement contrasté.
 * Pas d'ombre par défaut (dev tool → densité d'info, zéro décoration).
 * Utilise `padded` uniquement quand on veut le padding interne standard.
 */
export function Card({ children, className = '', padded = false }: CardProps) {
  return (
    <div
      className={
        'rounded-panel border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900 ' +
        (padded ? 'p-4 ' : '') +
        className
      }
    >
      {children}
    </div>
  );
}
