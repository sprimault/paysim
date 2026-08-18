// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { ChevronRight, Repeat } from 'lucide-react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { CopyButton } from '@/shared/ui/CopyButton';
import { DataTable, type Column } from '@/shared/ui/DataTable';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorBanner } from '@/shared/ui/ErrorBanner';
import { ListFilters, type FilterState } from '@/shared/ui/ListFilters';
import { ProviderTabs } from '@/shared/ui/ProviderTabs';
import { RefreshButton } from '@/shared/ui/RefreshButton';
import { Tooltip } from '@/shared/ui/Tooltip';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { useFormatRelative } from '@/shared/hooks/useFormatRelative';
import { useListFilters } from '@/shared/hooks/useListFilters';
import { truncate } from '@/shared/lib/strings';
import { useT } from '@/shared/i18n/useT';
import { useSubscriptionsList } from '@/entities/subscription/model/useSubscriptions';
import type { SubscriptionOutput } from '@/shared/model';

/** Un abonnement prélève encore, ou plus : il n'y a pas de troisième cas. */
const ETATS_ABONNEMENT: FilterState[] = [
  { value: 'active', labelKey: 'subscription.state.active' },
  { value: 'cancelled', labelKey: 'subscription.state.cancelled' },
];

/**
 * Liste des abonnements. Structure alignée sur PaymentList — table
 * dense, cross-provider par défaut (l'API retourne uniquement payzen
 * aujourd'hui, Stripe arrivera en phase 7).
 */
export function SubscriptionList() {
  const t = useT();
  const rel = useFormatRelative();
  const { subscriptions, loading, error, refresh } = useSubscriptionsList();
  const [providerFilter, setProviderFilter] = useState<string>('');

  const { query, setQuery, etats, setEtats, filtered, total } = useListFilters(subscriptions, {
    provider: providerFilter,
    providerOf: (s) => s.provider,
    searchFields: (s) => [s.orderId, s.id, s.paymentMethodToken],
    // Un abonnement n'a pas de champ d'état : il est actif tant qu'il
    // n'est pas annulé. On le dérive pour que le filtre s'exprime dans
    // le même vocabulaire que les badges de la liste.
    stateOf: (s) => (s.cancelled ? 'cancelled' : 'active'),
  });

  const columns: Column<SubscriptionOutput>[] = [
    {
      header: t('subscription.list.column.state'),
      // Les annulés au bout du tri croissant : ce qu'on cherche dans
      // cette liste, c'est ce qui prélève encore.
      sortValue: (s) => (s.cancelled ? 1 : 0),
      cell: (s) =>
        s.cancelled ? (
          <Badge tone="unpaid">{t('subscription.state.cancelled')}</Badge>
        ) : (
          <Badge tone="paid">{t('subscription.state.active')}</Badge>
        ),
    },
    {
      header: t('subscription.list.column.provider'),
      hideBelow: 'xl',
      sortValue: (s) => s.provider,
      cell: (s) => (
        <span className="text-xs text-zinc-500 dark:text-zinc-400">{s.provider}</span>
      ),
    },
    {
      header: t('subscription.list.column.amount'),
      align: 'right',
      sortValue: (s) => s.amount,
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
      header: t('subscription.list.column.order'),
      sortValue: (s) => s.orderId ?? '',
      cell: (s) => (
        <span className="text-sm text-zinc-700 dark:text-zinc-300">{s.orderId || '—'}</span>
      ),
    },
    {
      header: t('subscription.list.column.billings'),
      align: 'right',
      sortValue: (s) => s.billingCount,
      // Un abonnement à zéro échéance n'a encore rien prélevé : c'est
      // l'information la plus utile de la ligne quand on cherche
      // pourquoi une facturation récurrente ne tombe pas. Le zéro est
      // donc grisé plutôt que masqué.
      cell: (s) => (
        <span
          className={
            'font-mono text-sm tabular ' +
            (s.billingCount === 0
              ? 'text-zinc-400 dark:text-zinc-600'
              : 'text-zinc-900 dark:text-zinc-100')
          }
        >
          {s.billingCount}
        </span>
      ),
    },
    {
      header: t('subscription.list.column.rrule'),
      hideBelow: 'xl',
      cell: (s) => (
        <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
          {truncate(s.rrule || '—', 30)}
        </code>
      ),
    },
    {
      header: t('subscription.list.column.id'),
      hideBelow: 'lg',
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
      header: t('subscription.list.column.created'),
      hideBelow: 'lg',
      sortValue: (s) => s.createdAt,
      cell: (s) => (
        <Tooltip label={formatShort(s.createdAt)}>
          <span className="text-xs text-zinc-500 dark:text-zinc-400">{rel(s.createdAt)}</span>
        </Tooltip>
      ),
    },
    {
      header: t('subscription.list.column.actions'),
      srOnly: true,
      align: 'right',
      cell: (s) => (
        <Tooltip label={t('subscription.list.action.open')} focusExterne>
          <Link
            to={`/subscriptions/${s.id}`}
            className="inline-flex rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
            aria-label={t('subscription.list.action.open')}
          >
            <ChevronRight size={16} />
          </Link>
        </Tooltip>
      ),
    },
  ];

  const countLabel =
    loading && filtered.length === 0
      ? t('common.action.loading')
      : filtered.length === 0
        ? t('subscription.list.countZero')
        : filtered.length === 1
          ? t('subscription.list.countOne')
          : t('subscription.list.countMany', { count: filtered.length });

  return (
    <div className="mx-auto max-w-7xl px-6 py-6">
      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            {t('subscription.list.title')}
          </h1>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">{countLabel}</p>
        </div>
        <RefreshButton onRefresh={refresh} />
      </div>

      <ProviderTabs
        value={providerFilter}
        onChange={setProviderFilter}
        items={subscriptions}
        providerOf={(s) => s.provider}
      />

      {error && <ErrorBanner message={t('subscription.list.errorPrefix', { error })} />}

      <DataTable
        columns={columns}
        rows={filtered}
        rowKey={(s) => s.id}
        loading={loading}
        pageSize={10}
        totalRows={total}
        toolbar={
          <ListFilters
            query={query}
            onQueryChange={setQuery}
            placeholderKey="common.filters.searchSubscriptions"
            states={ETATS_ABONNEMENT}
            selected={etats}
            onSelectedChange={setEtats}
            shown={filtered.length}
            total={total}
          />
        }
        emptyState={
          <EmptyState
            icon={Repeat}
            title={t('subscription.list.empty.title')}
            hint={t('subscription.list.empty.hint')}
          />
        }
      />
    </div>
  );
}
