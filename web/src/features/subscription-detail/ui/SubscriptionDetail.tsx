// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Play, Ban, ArrowLeft } from 'lucide-react';
import { Link, useParams } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { Card } from '@/shared/ui/Card';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { CopyButton } from '@/shared/ui/CopyButton';
import { toast } from '@/shared/ui/toastStore';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { useSubscription } from '@/entities/subscription/model/useSubscriptions';
import {
  cancelSubscription,
  triggerBilling,
} from '@/entities/subscription/api/subscriptionApi';

/**
 * Vue détail d'un abonnement. Deux actions : trigger-billing (crée
 * un renewal, redirige vers le paiement produit) et cancel (annule
 * définitivement, désactive trigger-billing).
 */
export function SubscriptionDetail() {
  const { id } = useParams();
  const { subscription, loading, error, refresh } = useSubscription(id);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  if (!id) return null;
  if (loading && !subscription) {
    return <div className="mx-auto max-w-4xl px-6 py-6 text-sm text-zinc-500">Chargement…</div>;
  }
  if (error && !subscription) {
    return (
      <div className="mx-auto max-w-4xl px-6 py-6 text-sm text-rose-700 dark:text-rose-300">
        Impossible de charger l'abonnement : {error}
      </div>
    );
  }
  if (!subscription) return null;

  async function handleTrigger() {
    setBusy(true);
    try {
      const res = await triggerBilling(id!);
      toast.success('Échéance déclenchée', `Paiement ${res.state}`);
      // Rafraîchit la sub (state éventuellement modifié) et redirige
      // vers le paiement créé — l'utilisateur voit tout de suite le
      // résultat au niveau transaction.
      await refresh();
      window.location.hash = `#/payments/${res.paymentUuid}`;
    } catch (e) {
      toast.error('Trigger échoué', (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function handleCancel() {
    setBusy(true);
    try {
      await cancelSubscription(id!);
      toast.success('Abonnement annulé');
      await refresh();
    } catch (e) {
      toast.error('Annulation échouée', (e as Error).message);
    } finally {
      setBusy(false);
      setCancelOpen(false);
    }
  }

  return (
    <div className="mx-auto max-w-4xl px-6 py-6">
      <Link
        to="/subscriptions"
        className="mb-4 inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
      >
        <ArrowLeft size={14} /> Retour à la liste
      </Link>

      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            Abonnement
          </h1>
          <div className="mt-1 flex items-center gap-2">
            <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
              {subscription.id}
            </code>
            <CopyButton value={subscription.id} className="p-0.5" />
            {subscription.cancelled ? (
              <Badge tone="unpaid">Annulé</Badge>
            ) : (
              <Badge tone="paid">Actif</Badge>
            )}
          </div>
        </div>
        <div className="flex gap-2">
          {!subscription.cancelled && (
            <>
              <Button
                variant="primary"
                size="sm"
                leftIcon={<Play size={14} />}
                onClick={handleTrigger}
                disabled={busy}
              >
                Déclencher une échéance
              </Button>
              <Button
                variant="danger"
                size="sm"
                leftIcon={<Ban size={14} />}
                onClick={() => setCancelOpen(true)}
                disabled={busy}
              >
                Annuler
              </Button>
            </>
          )}
        </div>
      </div>

      <Card>
        <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
          <Field label="Provider" value={subscription.provider} />
          <Field
            label="Montant"
            value={
              <span className="font-mono tabular">
                {formatAmount(subscription.amount)}
                <span className="ml-1 text-xs text-zinc-500">{subscription.currency}</span>
              </span>
            }
          />
          <Field label="Order ID" value={subscription.orderId || '—'} />
          <Field label="Effect date" value={subscription.effectDate || '—'} />
          <Field
            label="RRule"
            value={<code className="font-mono text-xs">{subscription.rrule || '—'}</code>}
            wide
          />
          <Field
            label="Payment method token"
            value={
              <div className="flex items-center gap-1">
                <Link
                  to={`/payment-methods/${subscription.paymentMethodToken}`}
                  className="font-mono text-xs text-brand-700 hover:underline dark:text-brand-300"
                >
                  {subscription.paymentMethodToken}
                </Link>
                <CopyButton value={subscription.paymentMethodToken} className="p-0.5" />
              </div>
            }
            wide
          />
          <Field label="Créé le" value={formatShort(subscription.createdAt)} wide />
          {subscription.metadata && Object.keys(subscription.metadata).length > 0 && (
            <Field
              label="Metadata"
              value={
                <ul className="text-xs">
                  {Object.entries(subscription.metadata).map(([k, v]) => (
                    <li key={k}>
                      <code className="font-mono">
                        {k}={v}
                      </code>
                    </li>
                  ))}
                </ul>
              }
              wide
            />
          )}
        </dl>
      </Card>

      <ConfirmDialog
        open={cancelOpen}
        danger
        title="Annuler cet abonnement ?"
        description={
          <>
            Cette action est irréversible. Les futures échéances
            (<code>trigger_billing</code>) seront refusées.
          </>
        }
        confirmLabel="Annuler l'abonnement"
        loading={busy}
        onConfirm={handleCancel}
        onCancel={() => setCancelOpen(false)}
      />
    </div>
  );
}

function Field({
  label,
  value,
  wide,
}: {
  label: string;
  value: React.ReactNode;
  wide?: boolean;
}) {
  return (
    <div className={wide ? 'sm:col-span-2' : ''}>
      <dt className="text-xs uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
        {label}
      </dt>
      <dd className="mt-0.5 text-sm text-zinc-900 dark:text-zinc-100">{value}</dd>
    </div>
  );
}
