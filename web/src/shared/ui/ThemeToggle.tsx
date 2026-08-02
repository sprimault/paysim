// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Monitor, Moon, Sun } from 'lucide-react';
import type { ComponentType } from 'react';
import { useTheme } from '@/shared/hooks/useTheme';
import type { Theme } from '@/shared/lib/theme';

interface Option {
  value: Theme;
  label: string;
  icon: ComponentType<{ size?: number; className?: string }>;
}

const OPTIONS: readonly Option[] = [
  { value: 'light', label: 'Clair', icon: Sun },
  { value: 'system', label: 'Système', icon: Monitor },
  { value: 'dark', label: 'Sombre', icon: Moon },
];

/**
 * Sélecteur de thème compact — trois boutons radio (light/system/dark)
 * dans une pillule. Placé dans le Header, à côté du ConnectionIndicator.
 *
 * Semantics : `radiogroup` + `radio` avec `aria-checked` pour la
 * lisibilité screen-reader ; les icônes seules ne portent pas de texte
 * visible mais leur `title` sert de tooltip souris.
 */
export function ThemeToggle() {
  const { theme, setTheme } = useTheme();

  return (
    <div
      role="radiogroup"
      aria-label="Thème"
      className="inline-flex rounded-md border border-zinc-200 bg-white p-0.5 dark:border-zinc-800 dark:bg-zinc-900"
    >
      {OPTIONS.map(({ value, label, icon: Icon }) => {
        const active = theme === value;
        return (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={active}
            title={label}
            onClick={() => setTheme(value)}
            className={
              'inline-flex h-6 w-6 items-center justify-center rounded transition-colors ' +
              (active
                ? 'bg-zinc-100 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100'
                : 'text-zinc-500 hover:text-zinc-900 dark:text-zinc-500 dark:hover:text-zinc-100')
            }
          >
            <Icon size={14} />
          </button>
        );
      })}
    </div>
  );
}
