// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Check, Copy } from 'lucide-react';

interface CopyButtonProps {
  value: string;
  className?: string;
  label?: string; // texte optionnel affiché à côté de l'icône (sinon icône seule)
}

/**
 * Bouton icône « copier ». Feedback visuel 1200 ms sur succès. Geste
 * le plus répété en débogage (web.md) — reste minimal et neutre visuellement.
 */
export function CopyButton({ value, className = '', label }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      // clipboard peut échouer en http (permissions) — fallback silencieux.
      // Un vrai fallback (textarea + execCommand) ne vaut pas le code sur un
      // outil dev servi en https ou en localhost.
    }
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      aria-label={copied ? 'Copié' : 'Copier'}
      title={copied ? 'Copié' : 'Copier'}
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
  );
}
