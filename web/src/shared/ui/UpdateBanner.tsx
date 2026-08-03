// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { RefreshCw } from 'lucide-react';
import { useBuildVersion } from '@/shared/hooks/useBuildVersion';
import { useT } from '@/shared/i18n/useT';

/**
 * Bannière discrète (bottom-right) qui apparaît quand un nouveau build
 * du binaire Paysim a été détecté. Un click recharge la page pour
 * charger le nouveau bundle Vite — évite le hard reload manuel.
 */
export function UpdateBanner() {
  const t = useT();
  const { updateAvailable } = useBuildVersion();
  if (!updateAvailable) return null;

  return (
    <div
      role="status"
      className="fixed bottom-4 right-4 z-50 flex max-w-sm items-start gap-3 rounded-md border border-brand-200 bg-white p-3 shadow-lg dark:border-brand-700 dark:bg-zinc-900"
    >
      <RefreshCw
        size={18}
        className="mt-0.5 shrink-0 text-brand-600 dark:text-brand-400"
      />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-zinc-900 dark:text-zinc-100">
          {t('updateBanner.title')}
        </p>
        <p className="mt-0.5 text-xs text-zinc-600 dark:text-zinc-400">
          {t('updateBanner.hint')}
        </p>
        <button
          type="button"
          onClick={() => window.location.reload()}
          className="mt-2 inline-flex items-center rounded-md bg-brand-600 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-brand-700 focus:outline-none focus:ring-2 focus:ring-brand-500 focus:ring-offset-2 dark:focus:ring-offset-zinc-900"
        >
          {t('updateBanner.action')}
        </button>
      </div>
    </div>
  );
}
