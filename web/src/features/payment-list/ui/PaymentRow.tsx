// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { ChevronRight, Trash2 } from 'lucide-react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { CopyButton } from '@/shared/ui/CopyButton';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { useFormatRelative } from '@/shared/hooks/useFormatRelative';
import { truncate } from '@/shared/lib/strings';
import { paymentStateMeta } from '@/shared/lib/statusMeta';
import { useT } from '@/shared/i18n/useT';
import type { PaymentSummary } from '@/shared/model';

interface PaymentRowProps {
  payment: PaymentSummary;
  /**
   * Reçoit aussi le bouton cliqué : la confirmation s'ancre dessous, et
   * le déclencheur vit ici alors que la boîte est ouverte par le parent.
   */
  onDelete?: (payment: PaymentSummary, trigger: HTMLElement) => void;
  /**
   * Affiche la colonne provider quand true — utile dans l'onglet
   * « Tous » où le provider n'est pas déjà porté par le contexte de
   * l'onglet. Les onglets filtrés le laissent implicite pour ne pas
   * dupliquer une information redondante.
   */
  showProvider?: boolean;
}

export function PaymentRow({ payment: p, onDelete, showProvider }: PaymentRowProps) {
  const t = useT();
  const rel = useFormatRelative();
  const meta = paymentStateMeta[p.state];
  const StateIcon = meta.icon;

  return (
    <tr className="border-b border-zinc-200 last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900/50">
      <td className="px-4 py-2.5">
        <div className="flex items-center gap-1.5">
          <Badge tone={meta.tone} icon={<StateIcon size={12} />}>
            {t(meta.labelKey)}
          </Badge>
          {/*
            Le code du motif accolé à l'état, pas son libellé : « refusé »
            ne dit pas si l'on peut reconduire, alors qu'un 51 se retente
            et qu'un 43 impose de réclamer une autre carte. Le libellé
            tient dans l'infobulle — en colonne, il pousserait le tableau.

            Rien ne s'affiche quand le refus n'a pas de motif bancaire :
            un abandon ou une expiration n'ont pas de code, et un badge
            vide vaudrait moins que l'absence de badge.
          */}
          {p.declineCode && (
            <span
              title={p.declineMessage}
              className="rounded bg-rose-100 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-rose-700 dark:bg-rose-950/60 dark:text-rose-300"
            >
              {p.declineCode}
            </span>
          )}
        </div>
      </td>
      {showProvider && (
        <td className="px-4 py-2.5 text-xs text-zinc-500 dark:text-zinc-400">
          {p.provider}
        </td>
      )}
      <td className="px-4 py-2.5 text-right font-mono text-sm tabular text-zinc-900 dark:text-zinc-100">
        {formatAmount(p.amount)}
        <span className="ml-1 text-xs text-zinc-500 dark:text-zinc-500">{p.currency}</span>
      </td>
      <td className="px-4 py-2.5 text-sm text-zinc-700 dark:text-zinc-300">{p.orderId}</td>
      <td className="px-4 py-2.5">
        <div className="flex items-center gap-1">
          <Link
            to={`/payments/${p.uuid}`}
            className="font-mono text-xs text-brand-600 hover:underline dark:text-brand-400"
            onClick={(e) => e.stopPropagation()}
          >
            {truncate(p.uuid, 13)}
          </Link>
          <CopyButton value={p.uuid} className="p-0.5" />
        </div>
      </td>
      <td className="px-4 py-2.5">
        {p.paymentMethodToken ? (
          <div className="flex items-center gap-1">
            <Link
              to={`/payment-methods/${p.paymentMethodToken}`}
              className="font-mono text-xs text-brand-600 hover:underline dark:text-brand-400"
              onClick={(e) => e.stopPropagation()}
            >
              {truncate(p.paymentMethodToken, 13)}
            </Link>
            <CopyButton value={p.paymentMethodToken} className="p-0.5" />
          </div>
        ) : (
          <span className="text-xs text-zinc-300 dark:text-zinc-700">—</span>
        )}
      </td>
      <td
        className="px-4 py-2.5 text-xs text-zinc-500 dark:text-zinc-400"
        title={formatShort(p.createdAt)}
      >
        {rel(p.createdAt)}
      </td>
      <td
        className="px-4 py-2.5 text-xs text-zinc-500 dark:text-zinc-400"
        title={formatShort(p.updatedAt)}
      >
        {rel(p.updatedAt)}
      </td>
      <td className="px-4 py-2.5 text-right">
        <div className="inline-flex items-center gap-0.5">
          {onDelete && (
            <button
              type="button"
              onClick={(e) => onDelete(p, e.currentTarget)}
              aria-label={t('payment.list.action.deletePayment')}
              title={t('common.action.delete')}
              className="rounded p-1 text-zinc-400 hover:bg-rose-50 hover:text-rose-600 dark:hover:bg-rose-950/40 dark:hover:text-rose-400"
            >
              <Trash2 size={14} />
            </button>
          )}
          <Link
            to={`/payments/${p.uuid}`}
            className="rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
            aria-label={t('payment.list.action.openPayment')}
          >
            <ChevronRight size={16} />
          </Link>
        </div>
      </td>
    </tr>
  );
}
