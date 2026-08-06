// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect } from 'react';
import { useT } from '@/shared/i18n/useT';
import { SHORTCUT_DOCS, type ShortcutDoc } from '@/shared/model/shortcuts';

interface ShortcutsHelpProps {
  open: boolean;
  onClose: () => void;
}

/**
 * Aide-mémoire des raccourcis clavier, ouvert par `?`.
 *
 * Rendu comme un dialogue : `useKeyboardShortcuts` neutralise ses
 * écoutes tant qu'un `role="dialog"` est présent, ce qui empêche une
 * frappe destinée à l'aide de déclencher l'action qu'elle décrit.
 * La fermeture passe donc par Escape et le clic hors du panneau, pas
 * par la table des raccourcis.
 */
export function ShortcutsHelp({ open, onClose }: ShortcutsHelpProps) {
  const t = useT();

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  const globaux = SHORTCUT_DOCS.filter((s) => s.scope === 'global');
  const contextuels = SHORTCUT_DOCS.filter((s) => s.scope !== 'global');

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-zinc-900/40 p-4 backdrop-blur-sm"
      onClick={onClose}
      role="presentation"
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('shortcuts.title')}
        className="w-full max-w-md rounded-lg border border-zinc-200 bg-white p-5 shadow-xl dark:border-zinc-800 dark:bg-zinc-900"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="mb-4 text-sm font-semibold text-zinc-900 dark:text-zinc-100">
          {t('shortcuts.title')}
        </h2>

        <dl className="space-y-1.5">
          {globaux.map((s) => (
            <Ligne key={s.labelKey} doc={s} />
          ))}
        </dl>

        <h3 className="mt-5 mb-2 text-xs font-medium uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
          {t('shortcuts.scope.webhook')}
        </h3>
        <dl className="space-y-1.5">
          {contextuels.map((s) => (
            <Ligne key={s.labelKey} doc={s} />
          ))}
        </dl>

        <p className="mt-5 text-xs text-zinc-500 dark:text-zinc-400">
          {t('shortcuts.hint')}
        </p>
      </div>
    </div>
  );
}

function Ligne({ doc }: { doc: ShortcutDoc }) {
  const t = useT();
  const touches = Array.isArray(doc.keys) ? doc.keys : [doc.keys];
  return (
    <div className="flex items-baseline justify-between gap-4 text-sm">
      <dt className="text-zinc-600 dark:text-zinc-300">{t(doc.labelKey)}</dt>
      <dd className="flex shrink-0 items-center gap-1">
        {touches.map((k, i) => (
          <kbd
            key={i}
            className="rounded border border-zinc-300 bg-zinc-50 px-1.5 py-0.5 font-mono text-xs text-zinc-700 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200"
          >
            {k}
          </kbd>
        ))}
      </dd>
    </div>
  );
}
