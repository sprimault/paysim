// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Monitor, Moon, Sun } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import type { ComponentType } from 'react';
import { useTheme } from '@/shared/hooks/useTheme';
import type { Theme } from '@/shared/lib/theme';
import { useT } from '@/shared/i18n/useT';
import type { MessageKey } from '@/shared/i18n/messages';

interface Option {
  value: Theme;
  labelKey: MessageKey;
  icon: ComponentType<{ size?: number; className?: string }>;
}

// Ordre Clair / Sombre / Système — le mode « Système » vient en
// dernier parce qu'il représente un fallback plutôt qu'un choix
// explicite (« comme mon OS »), les deux modes utilisateurs directs
// tiennent la gauche.
const OPTIONS: readonly Option[] = [
  { value: 'light', labelKey: 'header.theme.light', icon: Sun },
  { value: 'dark', labelKey: 'header.theme.dark', icon: Moon },
  { value: 'system', labelKey: 'header.theme.system', icon: Monitor },
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
  const t = useT();

  return (
    <div
      role="radiogroup"
      aria-label={t('header.theme.label')}
      className="inline-flex rounded-md border border-zinc-200 bg-white p-0.5 dark:border-zinc-800 dark:bg-zinc-900"
    >
      {OPTIONS.map(({ value, labelKey, icon: Icon }) => {
        const active = theme === value;
        return (
          <Tooltip key={value} label={t(labelKey)} enfantFocusable>
            <button
              type="button"
              role="radio"
              aria-checked={active}
              aria-label={t(labelKey)}
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
          </Tooltip>
        );
      })}
    </div>
  );
}
