// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { useT } from '@/shared/i18n/useT';

interface RefreshButtonProps {
  /** Callback invoquée sur click — typiquement `refresh` d'un hook list. */
  onRefresh: () => Promise<void>;
  /** Titre du tooltip (défaut : common.action.refresh). */
  title?: string;
}

/**
 * Bouton icône « actualiser » — spin léger pendant le fetch, désactivé
 * pour éviter le double-click. Placé dans les header de listes
 * (PaymentList, SubscriptionList, PaymentMethodList).
 */
export function RefreshButton({ onRefresh, title }: RefreshButtonProps) {
  const t = useT();
  const tip = title ?? t('common.action.refresh');
  const [busy, setBusy] = useState(false);
  async function handle() {
    if (busy) return;
    setBusy(true);
    try {
      await onRefresh();
    } finally {
      setBusy(false);
    }
  }
  return (
    <Tooltip label={tip} enfantFocusable>
      <button
        type="button"
        onClick={handle}
        disabled={busy}
        aria-label={tip}
        className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-600 transition-colors hover:bg-zinc-50 hover:text-zinc-900 disabled:opacity-50 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
      >
        <RefreshCw size={14} className={busy ? 'animate-spin' : ''} />
      </button>
    </Tooltip>
  );
}
