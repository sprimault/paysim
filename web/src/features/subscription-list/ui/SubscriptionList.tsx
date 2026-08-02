// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useMemo, useState } from 'react';
import { ChevronRight, Repeat } from 'lucide-react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { CopyButton } from '@/shared/ui/CopyButton';
import { DataTable, type Column } from '@/shared/ui/DataTable';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ProviderTabs } from '@/shared/ui/ProviderTabs';
import { RefreshButton } from '@/shared/ui/RefreshButton';
import { formatAmount } from '@/shared/lib/numbers';
import { formatRelative, formatShort } from '@/shared/lib/dates';
import { truncate } from '@/shared/lib/strings';
import { useSubscriptionsList } from '@/entities/subscription/model/useSubscriptions';
import type { SubscriptionOutput } from '@/shared/model';

/**
 * Liste des abonnements. Structure alignée sur PaymentList — table
 * dense, cross-provider par défaut (l'API retourne uniquement payzen
 * aujourd'hui, Stripe arrivera en phase 5).
 */
export function SubscriptionList() {
  const { subscriptions, loading, error, refresh } = useSubscriptionsList();
  const [providerFilter, setProviderFilter] = useState<string>('');
  const filtered = useMemo(
    () =>
      providerFilter
        ? subscriptions.filter((s) => s.provider === providerFilter)
        : subscriptions,
    [subscriptions, providerFilter],
  );

  const columns: Column<SubscriptionOutput>[] = [
    {
      header: 'État',
      cell: (s) =>
        s.cancelled ? (
          <Badge tone="unpaid">Annulé</Badge>
        ) : (
          <Badge tone="paid">Actif</Badge>
        ),
    },
    {
      header: 'Provider',
      cell: (s) => (
        <span className="text-xs text-zinc-500 dark:text-zinc-400">{s.provider}</span>
      ),
    },
    {
      header: 'Montant',
      align: 'right',
      cell: (s) => (
        <span className="font-mono text-sm tabular text-zinc-900 dark:text-zinc-100">
          {formatAmount(s.amount)}
          <span className="ml-1 text-xs text-zinc-500 dark:text-zinc-500">
            {s.currency}
          </span>
        </span>
      ),
    },
    {
      header: 'Commande',
      cell: (s) => (
        <span className="text-sm text-zinc-700 dark:text-zinc-300">{s.orderId || '—'}</span>
      ),
    },
    {
      header: 'RRule',
      cell: (s) => (
        <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
          {truncate(s.rrule || '—', 30)}
        </code>
      ),
    },
    {
      header: 'ID',
      cell: (s) => (
        <div className="flex items-center gap-1">
          <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
            {truncate(s.id, 13)}
          </code>
          <CopyButton value={s.id} className="p-0.5" />
        </div>
      ),
    },
    {
      header: 'Créé',
      cell: (s) => (
        <span
          className="text-xs text-zinc-500 dark:text-zinc-400"
          title={formatShort(s.createdAt)}
        >
          {formatRelative(s.createdAt)}
        </span>
      ),
    },
    {
      header: 'Actions',
      srOnly: true,
      align: 'right',
      cell: (s) => (
        <Link
          to={`/subscriptions/${s.id}`}
          className="inline-flex rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
          aria-label="Ouvrir l'abonnement"
        >
          <ChevronRight size={16} />
        </Link>
      ),
    },
  ];

  return (
    <div className="mx-auto max-w-7xl px-6 py-6">
      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            Abonnements
          </h1>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">
            {loading && filtered.length === 0
              ? 'Chargement…'
              : `${filtered.length} abonnement${filtered.length > 1 ? 's' : ''} en mémoire`}
          </p>
        </div>
        <RefreshButton onRefresh={refresh} />
      </div>

      <ProviderTabs value={providerFilter} onChange={setProviderFilter} />

      {error && (
        <div className="mb-4 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300">
          Impossible de charger les abonnements : {error}
        </div>
      )}

      <DataTable
        columns={columns}
        rows={filtered}
        rowKey={(s) => s.id}
        loading={loading}
        emptyState={
          <EmptyState
            icon={Repeat}
            title="Aucun abonnement"
            hint="Les abonnements créés via l'API apparaîtront ici."
          />
        }
      />
    </div>
  );
}
