// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { CreditCard } from 'lucide-react';
import { EmptyState } from '@/shared/ui/EmptyState';
import { Skeleton } from '@/shared/ui/Skeleton';
import { usePaymentsList } from '@/entities/payment/model/usePayments';
import { PaymentRow } from './PaymentRow';

/**
 * Écran principal. Table dense — pas de cards ombrés. C'est ce qu'un
 * dev regarde en priorité pendant qu'il débogue autre chose (web.md).
 * Le SSE branché dans App.tsx alimente le store en continu ; ce
 * composant ne fait que lire et rendre.
 */
export function PaymentList() {
  const { payments, loading, error } = usePaymentsList();

  return (
    <div className="mx-auto max-w-7xl px-6 py-6">
      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            Paiements
          </h1>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">
            {loading && payments.length === 0
              ? 'Chargement…'
              : `${payments.length} paiement${payments.length > 1 ? 's' : ''} en mémoire`}
          </p>
        </div>
      </div>

      {error && (
        <div className="mb-4 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300">
          Impossible de charger les paiements : {error}
        </div>
      )}

      {loading && payments.length === 0 ? (
        <div className="rounded-panel border border-zinc-200 p-6 dark:border-zinc-800">
          <Skeleton count={5} />
        </div>
      ) : payments.length === 0 ? (
        <EmptyState
          icon={CreditCard}
          title="Aucun paiement"
          hint="Les paiements créés via l'API PayZen apparaîtront ici en temps réel."
        />
      ) : (
        <div className="overflow-hidden rounded-panel border border-zinc-200 dark:border-zinc-800">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-200 bg-zinc-50 text-left text-xs font-medium uppercase tracking-wider text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
                <th className="px-4 py-2">État</th>
                <th className="px-4 py-2 text-right tabular">Montant</th>
                <th className="px-4 py-2">Commande</th>
                <th className="px-4 py-2">UUID</th>
                <th className="px-4 py-2">Créé</th>
                <th className="px-4 py-2">Mis à jour</th>
                <th className="px-4 py-2 sr-only">Ouvrir</th>
              </tr>
            </thead>
            <tbody>
              {payments.map((p) => (
                <PaymentRow key={p.uuid} payment={p} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
