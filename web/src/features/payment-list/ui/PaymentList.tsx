// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { CreditCard, Trash2 } from 'lucide-react';
import { Button } from '@/shared/ui/Button';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { DataTable } from '@/shared/ui/DataTable';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorBanner } from '@/shared/ui/ErrorBanner';
import { ListFilters, type FilterState } from '@/shared/ui/ListFilters';
import { ProviderTabs } from '@/shared/ui/ProviderTabs';
import { RefreshButton } from '@/shared/ui/RefreshButton';
import { toast } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n/useT';
import { deletePayment, purgePayments } from '@/entities/payment/api/paymentApi';
import { usePaymentsList } from '@/entities/payment/model/usePayments';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import { useListFilters } from '@/shared/hooks/useListFilters';
import type { PaymentSummary } from '@/shared/model';
import { usePaymentColumns } from './paymentColumns';

/**
 * États proposés au filtre, dans l'ordre du cycle de vie.
 *
 * Sous-ensemble volontaire des huit états du domaine : ceux qu'on
 * cherche réellement en débogage. Un remboursement se trouve par sa
 * commande, pas en balayant une liste ; l'encombrement d'un bouton de
 * plus coûterait plus qu'il ne rendrait.
 */
const ETATS_PAIEMENT: FilterState[] = [
  { value: 'initiated', labelKey: 'payment.state.initiated' },
  { value: 'authorized', labelKey: 'payment.state.authorized' },
  { value: 'captured', labelKey: 'payment.state.captured' },
  { value: 'declined', labelKey: 'payment.state.declined' },
  { value: 'expired', labelKey: 'payment.state.expired' },
];

/**
 * Écran principal. Table dense — pas de cards ombrés. C'est ce qu'un
 * dev regarde en priorité pendant qu'il débogue autre chose (web.md).
 * Tabs de provider en tête pour préparer l'arrivée de Stripe : même
 * si un seul provider existe côté v1, l'UI signale l'extension.
 */
export function PaymentList() {
  const t = useT();
  const { payments, loading, error, refresh } = usePaymentsList();
  const removeFromStore = usePaymentStore((s) => s.remove);
  const [providerFilter, setProviderFilter] = useState<string>('');
  const [toDelete, setToDelete] = useState<PaymentSummary | null>(null);
  // Element cliqué : chaque boîte s'ancre sous son propre déclencheur.
  // La corbeille d'une ligne vit dans PaymentRow, le bouton de purge
  // ici — d'où deux ancres distinctes.
  const [ancreLigne, setAncreLigne] = useState<HTMLElement | null>(null);
  const [ancrePurge, setAncrePurge] = useState<HTMLElement | null>(null);
  const [purgeOpen, setPurgeOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const { query, setQuery, etats, setEtats, filtered, total } = useListFilters(payments, {
    provider: providerFilter,
    providerOf: (p) => p.provider,
    searchFields: (p) => [p.orderId, p.uuid, p.paymentMethodToken],
    stateOf: (p) => p.state,
  });

  const columns = usePaymentColumns({
    showProvider: providerFilter === '',
    onDelete: (payment, trigger) => {
      setAncreLigne(trigger);
      setToDelete(payment);
    },
  });

  async function handleDelete() {
    if (!toDelete) return;
    setBusy(true);
    try {
      await deletePayment(toDelete.uuid);
      // Optimiste : on retire du store tout de suite. L'event SSE
      // payment_deleted fera la même chose, no-op idempotent.
      removeFromStore(toDelete.uuid);
      toast.success(t('payment.list.toast.deleteSuccess'), toDelete.orderId);
    } catch (e) {
      toast.error(t('payment.list.toast.deleteError'), (e as Error).message);
    } finally {
      setBusy(false);
      setToDelete(null);
    }
  }

  async function handlePurge() {
    setBusy(true);
    try {
      const res = await purgePayments(providerFilter || undefined);
      toast.success(
        res.deleted === 1
          ? t('payment.list.toast.purgeSuccessOne')
          : t('payment.list.toast.purgeSuccessMany', { count: res.deleted }),
      );
      // L'event SSE payments_purged refetch la liste — pas de trim
      // local nécessaire.
      await refresh();
    } catch (e) {
      toast.error(t('payment.list.toast.purgeError'), (e as Error).message);
    } finally {
      setBusy(false);
      setPurgeOpen(false);
    }
  }

  const countLabel =
    loading && filtered.length === 0
      ? t('common.action.loading')
      : filtered.length === 0
        ? t('payment.list.countZero')
        : filtered.length === 1
          ? t('payment.list.countOne')
          : t('payment.list.countMany', { count: filtered.length });

  return (
    <div className="mx-auto max-w-7xl px-6 py-6">
      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            {t('payment.list.title')}
          </h1>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">{countLabel}</p>
        </div>
        <div className="flex items-center gap-2">
          <RefreshButton onRefresh={refresh} />
          {filtered.length > 0 && (
            <Button
              variant="danger"
              size="sm"
              leftIcon={<Trash2 size={14} />}
              onClick={(e) => {
                setAncrePurge(e.currentTarget);
                setPurgeOpen(true);
              }}
            >
              {providerFilter
                ? t('payment.list.action.purgeProvider', { provider: providerFilter })
                : t('payment.list.action.purgeAll')}
            </Button>
          )}
        </div>
      </div>

      <ProviderTabs value={providerFilter} onChange={setProviderFilter} />

      <ListFilters
        query={query}
        onQueryChange={setQuery}
        placeholderKey="common.filters.searchPayments"
        states={ETATS_PAIEMENT}
        selected={etats}
        onSelectedChange={setEtats}
        shown={filtered.length}
        total={total}
      />

      {error && <ErrorBanner message={t('payment.list.errorPrefix', { error })} />}

      <DataTable
        columns={columns}
        rows={filtered}
        rowKey={(p) => p.uuid}
        loading={loading}
        pageSize={10}
        emptyState={
          <EmptyState
            icon={CreditCard}
            title={t('payment.list.empty.title')}
            hint={t('payment.list.empty.hint')}
          />
        }
      />

      {/* Confirmations */}
      <ConfirmDialog
        open={!!toDelete}
        danger
        title={t('payment.list.dialog.deleteTitle')}
        description={t('payment.list.dialog.deleteDescription', {
          orderId: toDelete?.orderId ?? '',
        })}
        confirmLabel={t('common.action.delete')}
        loading={busy}
        onConfirm={handleDelete}
        onCancel={() => setToDelete(null)}
        anchorEl={ancreLigne}
      />
      <ConfirmDialog
        open={purgeOpen}
        danger
        title={
          providerFilter
            ? t('payment.list.dialog.purgeProviderTitle', { provider: providerFilter })
            : t('payment.list.dialog.purgeAllTitle')
        }
        description={
          providerFilter
            ? t('payment.list.dialog.purgeProviderDescription', { provider: providerFilter })
            : t('payment.list.dialog.purgeAllDescription')
        }
        confirmLabel={t('common.action.purge')}
        loading={busy}
        onConfirm={handlePurge}
        onCancel={() => setPurgeOpen(false)}
        anchorEl={ancrePurge}
      />
    </div>
  );
}

