// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Play, Ban, ArrowLeft } from 'lucide-react';
import { Link, useParams } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { Card } from '@/shared/ui/Card';
import { SubscriptionBillings } from './SubscriptionBillings';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { CopyButton } from '@/shared/ui/CopyButton';
import { ErrorBanner } from '@/shared/ui/ErrorBanner';
import { toast } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n/useT';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { useSubscription } from '@/entities/subscription/model/useSubscriptions';
import { Field } from '@/shared/ui/FicheField';
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
  const t = useT();
  const { id } = useParams();
  const { subscription, loading, error, refresh } = useSubscription(id);
  const [cancelOpen, setCancelOpen] = useState(false);
  // Element cliqué : la boîte s'ancre dessous plutôt qu'au centre.
  const [ancre, setAncre] = useState<HTMLElement | null>(null);
  const [busy, setBusy] = useState(false);

  if (!id) return null;
  if (loading && !subscription) {
    return <div className="mx-auto max-w-4xl px-6 py-6 text-sm text-zinc-500">{t('subscription.detail.loading')}</div>;
  }
  if (error && !subscription) {
    return (
      <ErrorBanner pleinePage message={t('subscription.detail.errorPrefix', { error })} />
    );
  }
  if (!subscription) return null;

  async function handleTrigger() {
    setBusy(true);
    try {
      const res = await triggerBilling(id!);
      toast.success(
        t('subscription.detail.toast.triggerSuccess'),
        t('subscription.detail.toast.triggerSuccessHint', { state: res.state }),
      );
      // Rafraîchit la sub (state éventuellement modifié) et redirige
      // vers le paiement créé — l'utilisateur voit tout de suite le
      // résultat au niveau transaction.
      await refresh();
      window.location.hash = `#/payments/${res.paymentUuid}`;
    } catch (e) {
      toast.error(t('subscription.detail.toast.triggerError'), (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function handleCancel() {
    setBusy(true);
    try {
      await cancelSubscription(id!);
      toast.success(t('subscription.detail.toast.cancelSuccess'));
      await refresh();
    } catch (e) {
      toast.error(t('subscription.detail.toast.cancelError'), (e as Error).message);
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
        <ArrowLeft size={14} /> {t('common.nav.backToList')}
      </Link>

      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            {t('subscription.detail.title')}
          </h1>
          <div className="mt-1 flex items-center gap-2">
            <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
              {subscription.id}
            </code>
            <CopyButton value={subscription.id} className="p-0.5" />
            {subscription.cancelled ? (
              <Badge tone="unpaid">{t('subscription.state.cancelled')}</Badge>
            ) : (
              <Badge tone="paid">{t('subscription.state.active')}</Badge>
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
                {t('subscription.detail.action.trigger')}
              </Button>
              <Button
                variant="danger"
                size="sm"
                leftIcon={<Ban size={14} />}
                onClick={(e) => {
                  setAncre(e.currentTarget);
                  setCancelOpen(true);
                }}
                disabled={busy}
              >
                {t('subscription.detail.action.cancel')}
              </Button>
            </>
          )}
        </div>
      </div>

      <Card>
        <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
          <Field label={t('subscription.detail.field.provider')} value={subscription.provider} />
          <Field
            label={t('subscription.detail.field.amount')}
            value={
              <span className="font-mono tabular">
                {formatAmount(subscription.amount)}
                <span className="ml-1 text-xs text-zinc-500">{subscription.currency}</span>
              </span>
            }
          />
          <Field label={t('subscription.detail.field.orderId')} value={subscription.orderId || '—'} />
          <Field label={t('subscription.detail.field.effectDate')} value={subscription.effectDate || '—'} />
          <Field
            label={t('subscription.detail.field.rrule')}
            value={<code className="font-mono text-xs">{subscription.rrule || '—'}</code>}
            wide
          />
          <Field
            label={t('subscription.detail.field.paymentMethodToken')}
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
          <Field label={t('subscription.detail.field.createdAt')} value={formatShort(subscription.createdAt)} wide />
          {subscription.metadata && Object.keys(subscription.metadata).length > 0 && (
            <Field
              label={t('subscription.detail.field.metadata')}
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

      <SubscriptionBillings subscriptionId={subscription.id} />

      <ConfirmDialog
        open={cancelOpen}
        danger
        title={t('subscription.detail.dialog.cancelTitle')}
        description={t('subscription.detail.dialog.cancelDescription')}
        confirmLabel={t('subscription.detail.dialog.cancelConfirm')}
        loading={busy}
        onConfirm={handleCancel}
        onCancel={() => setCancelOpen(false)}
        anchorEl={ancre}
      />
    </div>
  );
}

