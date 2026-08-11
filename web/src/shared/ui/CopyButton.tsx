// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { toast } from '@/shared/ui/toastStore';
import { copyToClipboard } from '@/shared/lib/clipboard';
import { useT } from '@/shared/i18n/useT';

interface CopyButtonProps {
  value: string;
  className?: string;
  label?: string; // texte optionnel affiché à côté de l'icône (sinon icône seule)
  /**
   * Remplace le « Copier » du tooltip au repos, quand l'icône seule ne
   * dit pas ce qui part au presse-papier. Le retour de succès reste
   * « Copié » : c'est l'action qui varie, pas sa confirmation.
   */
  tip?: string;
}

/**
 * Bouton icône « copier ». Feedback visuel 1200 ms sur succès. Geste
 * le plus répété en débogage (web.md) — reste minimal et neutre visuellement.
 */
export function CopyButton({ value, className = '', label, tip: tipRepos }: CopyButtonProps) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const tip = copied ? t('common.action.copied') : (tipRepos ?? t('common.action.copy'));

  async function handleCopy() {
    if (await copyToClipboard(value)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
      return;
    }
    // Un échec muet est pire que pas de bouton : on croit avoir copié,
    // et on colle autre chose.
    toast.error(t('common.action.copyFailed'));
  }

  return (
    <Tooltip label={tip} focusExterne>
      <button
        type="button"
        onClick={handleCopy}
        aria-label={tip}
        className={
        'inline-flex items-center gap-1 rounded p-1 text-zinc-500 ' +
        'hover:bg-zinc-100 hover:text-zinc-800 ' +
        'dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-200 ' +
        'transition-colors ' +
        className
      }
    >
        {copied ? (
          <Check size={14} className="text-emerald-600 dark:text-emerald-400" />
        ) : (
          <Copy size={14} />
        )}
        {label && <span className="text-xs">{label}</span>}
      </button>
    </Tooltip>
  );
}
