// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  hint?: string;
  action?: ReactNode;
}

/**
 * EmptyState — placeholder pour les listes vides (aucun paiement, aucun
 * webhook, etc.). Icône Lucide XL en tête, titre, hint optionnel, CTA
 * optionnel. Discret, centré, sans ombre.
 */
export function EmptyState({ icon: Icon, title, hint, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
      <Icon size={40} className="text-zinc-400 dark:text-zinc-600" strokeWidth={1.5} />
      <h3 className="mt-4 text-sm font-medium text-zinc-900 dark:text-zinc-100">{title}</h3>
      {hint && (
        <p className="mt-1 max-w-sm text-sm text-zinc-500 dark:text-zinc-400">{hint}</p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
