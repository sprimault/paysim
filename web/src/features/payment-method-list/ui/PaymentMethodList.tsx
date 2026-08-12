// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { ChevronRight, CreditCard } from 'lucide-react';
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
import { formatShort } from '@/shared/lib/dates';
import { useFormatRelative } from '@/shared/hooks/useFormatRelative';
import { useListFilters } from '@/shared/hooks/useListFilters';
import { truncate } from '@/shared/lib/strings';
import { useT } from '@/shared/i18n/useT';
import { paymentMethodStatus } from '@/entities/payment-method/lib/status';
import { usePaymentMethodsList } from '@/entities/payment-method/model/usePaymentMethods';
import type { PaymentMethodOutput } from '@/shared/model';

/** Les trois verdicts d'exploitabilité, mêmes valeurs que les badges. */
const ETATS_MOYEN: FilterState[] = [
  { value: 'active', labelKey: 'paymentMethod.state.active' },
  { value: 'expired', labelKey: 'paymentMethod.state.expired' },
  { value: 'revoked', labelKey: 'paymentMethod.state.revoked' },
];

/**
 * Liste des moyens de paiement enregistrés. Réutilise le DataTable
 * shared (règle de trois validée : payments + subs + methods).
 *
 * Le PAN affiché est **toujours masqué** — c'est ce que Paysim retourne
 * par l'API (le PAN complet stocké côté serveur n'est jamais exposé,
 * même sur un simulateur).
 */
export function PaymentMethodList() {
  const t = useT();
  const rel = useFormatRelative();
  const { methods, loading, error, refresh } = usePaymentMethodsList();
  const [providerFilter, setProviderFilter] = useState<string>('');

  const { query, setQuery, etats, setEtats, filtered, total } = useListFilters(methods, {
    provider: providerFilter,
    providerOf: (m) => m.provider,
    // Le PAN masqué se cherche aussi : c'est par les quatre derniers
    // chiffres qu'un marchand désigne une carte.
    searchFields: (m) => [m.token, m.panMasked, m.brand],
    // Même verdict que celui affiché en badge — révoqué prime sur
    // expiré, et le filtre ne doit pas dire autre chose que la colonne.
    stateOf: paymentMethodStatus,
  });

  const columns: Column<PaymentMethodOutput>[] = [
    {
      header: t('paymentMethod.list.column.state'),
      // Actifs d'abord : les moyens inexploitables sont ce qu'on écarte,
      // pas ce qu'on cherche.
      sortValue: (m) => paymentMethodStatus(m),
      cell: (m) => {
        // Trois états visuels — cf. entities/payment-method/lib/status.
        // Révoqué prime sur expiré ; les deux empêchent un charge_token
        // ou trigger_billing d'aboutir.
        const s = paymentMethodStatus(m);
        if (s === 'revoked') return <Badge tone="unpaid">{t('paymentMethod.state.revoked')}</Badge>;
        if (s === 'expired') return <Badge tone="expired">{t('paymentMethod.state.expired')}</Badge>;
        return <Badge tone="paid">{t('paymentMethod.state.active')}</Badge>;
      },
    },
    {
      header: t('paymentMethod.list.column.provider'),
      hideBelow: 'xl',
      sortValue: (m) => m.provider,
      cell: (m) => (
        <span className="text-xs text-zinc-500 dark:text-zinc-400">{m.provider}</span>
      ),
    },
    {
      header: t('paymentMethod.list.column.brand'),
      sortValue: (m) => m.brand ?? '',
      cell: (m) => (
        <span className="text-sm text-zinc-700 dark:text-zinc-300">{m.brand || '—'}</span>
      ),
    },
    {
      header: t('paymentMethod.list.column.pan'),
      cell: (m) => (
        <code className="font-mono text-xs text-zinc-700 dark:text-zinc-300">
          {m.panMasked}
        </code>
      ),
    },
    {
      header: t('paymentMethod.list.column.expiry'),
      // Année puis mois : trier sur « 12/2026 » comme une chaîne mettrait
      // décembre avant janvier de l'année suivante.
      sortValue: (m) => m.expiryYear * 100 + m.expiryMonth,
      cell: (m) => (
        <span className="text-sm text-zinc-700 dark:text-zinc-300 tabular">
          {String(m.expiryMonth).padStart(2, '0')}/{m.expiryYear}
        </span>
      ),
    },
    {
      header: t('paymentMethod.list.column.token'),
      hideBelow: 'lg',
      cell: (m) => (
        <div className="flex items-center gap-1">
          <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
            {truncate(m.token, 13)}
          </code>
          <CopyButton value={m.token} className="p-0.5" />
        </div>
      ),
    },
    {
      header: t('paymentMethod.list.column.created'),
      hideBelow: 'lg',
      sortValue: (m) => m.createdAt,
      cell: (m) => (
        <Tooltip label={formatShort(m.createdAt)}>
          <span className="text-xs text-zinc-500 dark:text-zinc-400">{rel(m.createdAt)}</span>
        </Tooltip>
      ),
    },
    {
      header: t('paymentMethod.list.column.actions'),
      srOnly: true,
      align: 'right',
      cell: (m) => (
        <Tooltip label={t('paymentMethod.list.action.open')} focusExterne>
          <Link
            to={`/payment-methods/${m.token}`}
            className="inline-flex rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
            aria-label={t('paymentMethod.list.action.open')}
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
        ? t('paymentMethod.list.countZero')
        : filtered.length === 1
          ? t('paymentMethod.list.countOne')
          : t('paymentMethod.list.countMany', { count: filtered.length });

  return (
    <div className="mx-auto max-w-7xl px-6 py-6">
      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            {t('paymentMethod.list.title')}
          </h1>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">{countLabel}</p>
        </div>
        <RefreshButton onRefresh={refresh} />
      </div>

      <ProviderTabs value={providerFilter} onChange={setProviderFilter} />

      {error && <ErrorBanner message={t('paymentMethod.list.errorPrefix', { error })} />}

      <DataTable
        columns={columns}
        rows={filtered}
        rowKey={(m) => m.token}
        loading={loading}
        pageSize={10}
        totalRows={total}
        toolbar={
          <ListFilters
            query={query}
            onQueryChange={setQuery}
            placeholderKey="common.filters.searchMethods"
            states={ETATS_MOYEN}
            selected={etats}
            onSelectedChange={setEtats}
            shown={filtered.length}
            total={total}
          />
        }
        emptyState={
          <EmptyState
            icon={CreditCard}
            title={t('paymentMethod.list.empty.title')}
            hint={t('paymentMethod.list.empty.hint')}
          />
        }
      />
    </div>
  );
}
