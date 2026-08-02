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
  const { payments, loading, error, refresh } = usePaymentsList();
  const removeFromStore = usePaymentStore((s) => s.remove);
  const [providerFilter, setProviderFilter] = useState<string>('');
  const [toDelete, setToDelete] = useState<PaymentSummary | null>(null);
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
      toast.success('Paiement supprimé', toDelete.orderId);
    } catch (e) {
      toast.error('Suppression échouée', (e as Error).message);
    } finally {
      setBusy(false);
      setToDelete(null);
    }
  }

  async function handlePurge() {
    setBusy(true);
    try {
      const res = await purgePayments(providerFilter || undefined);
      toast.success(`${res.deleted} paiement${res.deleted > 1 ? 's' : ''} supprimé${res.deleted > 1 ? 's' : ''}`);
      // L'event SSE payments_purged refetch la liste — pas de trim
      // local nécessaire.
      await refresh();
    } catch (e) {
      toast.error('Purge échouée', (e as Error).message);
    } finally {
      setBusy(false);
      setPurgeOpen(false);
    }
  }

  return (
    <div className="mx-auto max-w-7xl px-6 py-6">
      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            Paiements
          </h1>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">
            {loading && filtered.length === 0
              ? 'Chargement…'
              : `${filtered.length} paiement${filtered.length > 1 ? 's' : ''} en mémoire`}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <RefreshButton onRefresh={refresh} />
          {filtered.length > 0 && (
            <Button
              variant="danger"
              size="sm"
              leftIcon={<Trash2 size={14} />}
              onClick={() => setPurgeOpen(true)}
            >
              {providerFilter ? `Vider ${providerFilter}` : 'Vider tout'}
            </Button>
          )}
        </div>
      </div>

      <ProviderTabs value={providerFilter} onChange={setProviderFilter} />

      {error && (
        <div className="mb-4 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300">
          Impossible de charger les paiements : {error}
        </div>
      )}

      {loading && filtered.length === 0 ? (
        <div className="rounded-panel border border-zinc-200 p-6 dark:border-zinc-800">
          <Skeleton count={5} />
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={CreditCard}
          title="Aucun paiement"
          hint="Les paiements créés via l'API apparaîtront ici en temps réel."
        />
      ) : (
        <div className="overflow-hidden rounded-panel border border-zinc-200 dark:border-zinc-800">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-200 bg-zinc-50 text-left text-xs font-medium uppercase tracking-wider text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
                <th className="px-4 py-2">État</th>
                {providerFilter === '' && <th className="px-4 py-2">Provider</th>}
                <th className="px-4 py-2 text-right tabular">Montant</th>
                <th className="px-4 py-2">Commande</th>
                <th className="px-4 py-2">UUID</th>
                <th className="px-4 py-2">Créé</th>
                <th className="px-4 py-2">Mis à jour</th>
                <th className="px-4 py-2 sr-only">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((p) => (
                <PaymentRow
                  key={p.uuid}
                  payment={p}
                  onDelete={setToDelete}
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
        title="Supprimer ce paiement ?"
        description={
          <>
            Le paiement <strong>{toDelete?.orderId}</strong> et ses événements seront
            supprimés. Cette action est irréversible.
          </>
        }
        confirmLabel="Supprimer"
        loading={busy}
        onConfirm={handleDelete}
        onCancel={() => setToDelete(null)}
      />
      <ConfirmDialog
        open={purgeOpen}
        danger
        title={providerFilter ? `Vider les paiements ${providerFilter} ?` : 'Vider tous les paiements ?'}
        description={
          providerFilter ? (
            <>
              Tous les paiements du provider <strong>{providerFilter}</strong> seront
              supprimés, ainsi que leurs événements.
            </>
          ) : (
            <>Tous les paiements de tous les providers seront supprimés.</>
          )
        }
        confirmLabel="Vider"
        loading={busy}
        onConfirm={handlePurge}
        onCancel={() => setPurgeOpen(false)}
      />
    </div>
  );
}

