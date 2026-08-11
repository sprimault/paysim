// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Languages } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { useLangStore } from '@/shared/i18n/store';
import { useT } from '@/shared/i18n/useT';
import type { Lang } from '@/shared/i18n/messages';

/**
 * LangToggle bascule entre FR et EN via un bouton segmenté compact
 * dans le Header, à droite du ThemeToggle.
 *
 * Deux valeurs uniquement — pas de segmented control à trois états
 * comme le theme, un simple bouton FR/EN suffit.
 */
export function LangToggle() {
  const lang = useLangStore((s) => s.lang);
  const setLang = useLangStore((s) => s.setLang);
  const t = useT();

  return (
    <div
      className="inline-flex items-center rounded-md border border-zinc-200 bg-white p-0.5 dark:border-zinc-800 dark:bg-zinc-900"
      role="group"
      aria-label={t('header.lang.label')}
    >
      <Languages
        size={14}
        className="ml-1 mr-0.5 text-zinc-400 dark:text-zinc-500"
        aria-hidden
      />
      <Option value="fr" current={lang} onSelect={setLang} label={t('header.lang.french')} short="FR" />
      <Option value="en" current={lang} onSelect={setLang} label={t('header.lang.english')} short="EN" />
    </div>
  );
}

function Option({
  value,
  current,
  onSelect,
  label,
  short,
}: {
  value: Lang;
  current: Lang;
  onSelect: (l: Lang) => void;
  label: string;
  short: string;
}) {
  const active = value === current;
  return (
    <Tooltip label={label} focusExterne>
      <button
        type="button"
        onClick={() => onSelect(value)}
        aria-pressed={active}
        aria-label={label}
        className={
          'rounded px-1.5 py-0.5 text-xs font-medium transition-colors ' +
          (active
            ? 'bg-brand-100 text-brand-800 dark:bg-brand-900/40 dark:text-brand-300'
            : 'text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-100')
        }
      >
        {short}
      </button>
    </Tooltip>
  );
}
