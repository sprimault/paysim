// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { ChevronRight, CreditCard } from 'lucide-react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { CopyButton } from '@/shared/ui/CopyButton';
import { DataTable, type Column } from '@/shared/ui/DataTable';
import { EmptyState } from '@/shared/ui/EmptyState';
import { formatRelative, formatShort } from '@/shared/lib/dates';
import { truncate } from '@/shared/lib/strings';
import { usePaymentMethodsList } from '@/entities/payment-method/model/usePaymentMethods';
import type { PaymentMethodOutput } from '@/shared/model';

/**
 * Liste des moyens de paiement enregistrés. Réutilise le DataTable
 * shared (règle de trois validée : payments + subs + methods).
 *
 * Le PAN affiché est **toujours masqué** — c'est ce que Paysim retourne
 * par l'API (le PAN complet stocké côté serveur n'est jamais exposé,
 * même sur un simulateur).
 */
export function PaymentMethodList() {
  const { methods, loading, error } = usePaymentMethodsList();

  const columns: Column<PaymentMethodOutput>[] = [
    {
      header: 'État',
      cell: (m) =>
        m.revoked ? (
          <Badge tone="unpaid">Révoqué</Badge>
        ) : (
          <Badge tone="paid">Actif</Badge>
        ),
    },
    {
      header: 'Provider',
      cell: (m) => (
        <span className="text-xs text-zinc-500 dark:text-zinc-400">{m.provider}</span>
      ),
    },
    {
      header: 'Marque',
      cell: (m) => (
        <span className="text-sm text-zinc-700 dark:text-zinc-300">{m.brand || '—'}</span>
      ),
    },
    {
      header: 'PAN',
      cell: (m) => (
        <code className="font-mono text-xs text-zinc-700 dark:text-zinc-300">
          {m.panMasked}
        </code>
      ),
    },
    {
      header: 'Expiration',
      cell: (m) => (
        <span className="text-sm text-zinc-700 dark:text-zinc-300 tabular">
          {String(m.expiryMonth).padStart(2, '0')}/{m.expiryYear}
        </span>
      ),
    },
    {
      header: 'Token',
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
      header: 'Créé',
      cell: (m) => (
        <span
          className="text-xs text-zinc-500 dark:text-zinc-400"
          title={formatShort(m.createdAt)}
        >
          {formatRelative(m.createdAt)}
        </span>
      ),
    },
    {
      header: 'Actions',
      srOnly: true,
      align: 'right',
      cell: (m) => (
        <Link
          to={`/payment-methods/${m.token}`}
          className="inline-flex rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
          aria-label="Ouvrir le moyen de paiement"
        >
          <ChevronRight size={16} />
        </Link>
      ),
    },
  ];

  return (
    <div className="mx-auto max-w-7xl px-6 py-6">
      <div className="mb-4">
        <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
          Moyens de paiement
        </h1>
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          {loading && methods.length === 0
            ? 'Chargement…'
            : `${methods.length} moyen${methods.length > 1 ? 's' : ''} enregistré${methods.length > 1 ? 's' : ''}`}
        </p>
      </div>

      {error && (
        <div className="mb-4 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-300">
          Impossible de charger les moyens de paiement : {error}
        </div>
      )}

      <DataTable
        columns={columns}
        rows={methods}
        rowKey={(m) => m.token}
        loading={loading}
        emptyState={
          <EmptyState
            icon={CreditCard}
            title="Aucun moyen de paiement enregistré"
            hint="Les moyens de paiement créés via un create_payment avec REGISTER_PAY apparaîtront ici."
          />
        }
      />
    </div>
  );
}
