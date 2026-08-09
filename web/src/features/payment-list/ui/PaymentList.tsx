// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useMemo, useState } from 'react';
import { CreditCard, Trash2 } from 'lucide-react';
import { Button } from '@/shared/ui/Button';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ProviderTabs } from '@/shared/ui/ProviderTabs';
import { RefreshButton } from '@/shared/ui/RefreshButton';
import { Skeleton } from '@/shared/ui/Skeleton';
import { toast } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n/useT';
import { deletePayment, purgePayments } from '@/entities/payment/api/paymentApi';
import { usePaymentsList } from '@/entities/payment/model/usePayments';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import type { PaymentSummary } from '@/shared/model';
import { PaymentRow } from './PaymentRow';

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

  const filtered = useMemo(() => {
    if (!providerFilter) return payments;
    return payments.filter((p) => p.provider === providerFilter);
  }, [payments, providerFilter]);

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

      {error && (
        <div className="mb-4 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300">
          {t('payment.list.errorPrefix', { error })}
        </div>
      )}

      {loading && filtered.length === 0 ? (
        <div className="rounded-panel border border-zinc-200 p-6 dark:border-zinc-800">
          <Skeleton count={5} />
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={CreditCard}
          title={t('payment.list.empty.title')}
          hint={t('payment.list.empty.hint')}
        />
      ) : (
        <div className="overflow-hidden rounded-panel border border-zinc-200 dark:border-zinc-800">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-200 bg-zinc-50 text-left text-xs font-medium uppercase tracking-wider text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
                <th className="px-4 py-2">{t('payment.list.column.state')}</th>
                {providerFilter === '' && <th className="px-4 py-2">{t('payment.list.column.provider')}</th>}
                <th className="px-4 py-2 text-right tabular">{t('payment.list.column.amount')}</th>
                <th className="px-4 py-2">{t('payment.list.column.order')}</th>
                <th className="px-4 py-2">{t('payment.list.column.uuid')}</th>
                <th className="px-4 py-2">{t('payment.list.column.paymentMethod')}</th>
                <th className="px-4 py-2">{t('payment.list.column.created')}</th>
                <th className="px-4 py-2">{t('payment.list.column.updated')}</th>
                <th className="px-4 py-2 sr-only">{t('payment.list.column.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((p) => (
                <PaymentRow
                  key={p.uuid}
                  payment={p}
                  onDelete={(payment, trigger) => {
                    setAncreLigne(trigger);
                    setToDelete(payment);
                  }}
                  showProvider={providerFilter === ''}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

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

