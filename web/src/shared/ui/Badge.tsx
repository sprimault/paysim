// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';

type Tone = 'paid' | 'unpaid' | 'authorised' | 'expired' | 'chargeback' | 'abandoned' | 'neutral';

interface BadgeProps {
  tone?: Tone;
  children: ReactNode;
  icon?: ReactNode;
  className?: string;
}

// Palette dérivée de tailwind.config.js#colors.status. Chaque tone porte
// un couple bg/texte pour clair et sombre. Les valeurs sont volontairement
// discrètes — un dev ne doit pas être ébloui par un dashboard rempli de
// pastilles fluo pendant qu'il débogue autre chose (web.md).
const toneClasses: Record<Tone, string> = {
  paid: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300',
  unpaid: 'bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-300',
  authorised: 'bg-sky-100 text-sky-800 dark:bg-sky-900/40 dark:text-sky-300',
  expired: 'bg-zinc-200 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300',
  chargeback: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300',
  abandoned: 'bg-zinc-100 text-zinc-600 dark:bg-zinc-800/60 dark:text-zinc-400',
  neutral: 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300',
};

export function Badge({ tone = 'neutral', children, icon, className = '' }: BadgeProps) {
  return (
    <span
      className={
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ' +
        toneClasses[tone] +
        ' ' +
        className
      }
    >
      {icon}
      {children}
    </span>
  );
}
