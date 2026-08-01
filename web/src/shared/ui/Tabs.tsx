// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';

interface TabDef {
  id: string;
  label: string;
  badge?: ReactNode; // compteur ou pastille à droite du label
}

interface TabsProps {
  tabs: TabDef[];
  active: string;
  onChange: (id: string) => void;
  className?: string;
}

/**
 * Tabs — barre d'onglets simple soulignée. Contrôlée : l'onglet actif
 * vient du parent (habituellement lié à l'URL via useSearchParams pour
 * que le lien soit partageable — cf. web.md).
 */
export function Tabs({ tabs, active, onChange, className = '' }: TabsProps) {
  return (
    <div
      className={
        'flex gap-1 border-b border-zinc-200 dark:border-zinc-800 ' + className
      }
      role="tablist"
    >
      {tabs.map((t) => {
        const isActive = t.id === active;
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={isActive}
            onClick={() => onChange(t.id)}
            className={
              'inline-flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm font-medium ' +
              'transition-colors ' +
              (isActive
                ? 'border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-300'
                : 'border-transparent text-zinc-500 hover:border-zinc-300 hover:text-zinc-800 ' +
                  'dark:text-zinc-400 dark:hover:border-zinc-700 dark:hover:text-zinc-200')
            }
          >
            {t.label}
            {t.badge}
          </button>
        );
      })}
    </div>
  );
}
