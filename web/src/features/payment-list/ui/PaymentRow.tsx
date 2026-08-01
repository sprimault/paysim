// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { ChevronRight } from 'lucide-react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { CopyButton } from '@/shared/ui/CopyButton';
import { formatAmount } from '@/shared/lib/numbers';
import { formatRelative, formatShort } from '@/shared/lib/dates';
import { truncate } from '@/shared/lib/strings';
import { paymentStateMeta } from '@/shared/lib/statusMeta';
import type { PaymentSummary } from '@/shared/model';

interface PaymentRowProps {
  payment: PaymentSummary;
}

export function PaymentRow({ payment: p }: PaymentRowProps) {
  const meta = paymentStateMeta[p.state];
  const StateIcon = meta.icon;

  return (
    <tr className="border-b border-zinc-200 last:border-b-0 hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900/50">
      <td className="px-4 py-2.5">
        <Badge tone={meta.tone} icon={<StateIcon size={12} />}>
          {meta.label}
        </Badge>
      </td>
      <td className="px-4 py-2.5 text-right font-mono text-sm tabular text-zinc-900 dark:text-zinc-100">
        {formatAmount(p.amount)}
        <span className="ml-1 text-xs text-zinc-500 dark:text-zinc-500">{p.currency}</span>
      </td>
      <td className="px-4 py-2.5 text-sm text-zinc-700 dark:text-zinc-300">{p.orderId}</td>
      <td className="px-4 py-2.5">
        <div className="flex items-center gap-1">
          <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
            {truncate(p.uuid, 13)}
          </code>
          <CopyButton value={p.uuid} className="p-0.5" />
        </div>
      </td>
      <td
        className="px-4 py-2.5 text-xs text-zinc-500 dark:text-zinc-400"
        title={formatShort(p.createdAt)}
      >
        {formatRelative(p.createdAt)}
      </td>
      <td
        className="px-4 py-2.5 text-xs text-zinc-500 dark:text-zinc-400"
        title={formatShort(p.updatedAt)}
      >
        {formatRelative(p.updatedAt)}
      </td>
      <td className="px-4 py-2.5 text-right">
        <Link
          to={`/payments/${p.uuid}`}
          className="inline-flex items-center gap-0.5 rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
          aria-label="Ouvrir le paiement"
        >
          <ChevronRight size={16} />
        </Link>
      </td>
    </tr>
  );
}
