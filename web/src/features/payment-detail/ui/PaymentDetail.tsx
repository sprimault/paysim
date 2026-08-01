// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { ArrowLeft, Trash2 } from 'lucide-react';
import { Link, useNavigate, useParams } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { CopyButton } from '@/shared/ui/CopyButton';
import { Skeleton } from '@/shared/ui/Skeleton';
import { Tabs } from '@/shared/ui/Tabs';
import { toast } from '@/shared/ui/toastStore';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { paymentStateMeta } from '@/shared/lib/statusMeta';
import { deletePayment } from '@/entities/payment/api/paymentApi';
import { usePayment } from '@/entities/payment/model/usePayments';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import { useWebhooksList } from '@/entities/webhook/model/useWebhooks';
import { TAB_IDS, TAB_LABELS, TAB_WITH_COUNTER, type TabId } from '@/features/payment-detail/model/tabs';
import { PaymentOverview } from './PaymentOverview';
import { PaymentTimeline } from './PaymentTimeline';
import { PaymentWebhooks } from './PaymentWebhooks';
import { PaymentPayload } from './PaymentPayload';

export function PaymentDetail() {
  const { uuid = '' } = useParams();
  const navigate = useNavigate();
  const { payment, loading, error } = usePayment(uuid);
  const removeFromStore = usePaymentStore((s) => s.remove);
  // Toute la liste des webhooks — pas de filtre par uuid côté API v1.
  // Le tab Webhooks affiche l'ensemble avec une note explicite.
  const { webhooks } = useWebhooksList();
  const [tab, setTab] = useState<TabId>('overview');
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  async function handleDelete() {
    setDeleting(true);
    try {
      await deletePayment(uuid);
      removeFromStore(uuid);
      toast.success('Paiement supprimé');
      navigate('/', { replace: true });
    } catch (e) {
      toast.error('Suppression échouée', (e as Error).message);
      setDeleting(false);
      setDeleteOpen(false);
    }
  }

  if (loading && !payment) {
    return (
      <div className="mx-auto max-w-6xl px-6 py-6">
        <Skeleton className="mb-3 h-4 w-32" />
        <Skeleton className="mb-6 h-10 w-64" />
        <Skeleton count={3} />
      </div>
    );
  }

  if (error || !payment) {
    return (
      <div className="mx-auto max-w-4xl px-6 py-16 text-center">
        <p className="text-sm text-zinc-500">
          {error ? `Erreur : ${error}` : `Paiement introuvable : ${uuid}`}
        </p>
        <Link
          to="/"
          className="mt-4 inline-flex items-center gap-1 text-sm text-brand-600 hover:underline"
        >
          <ArrowLeft size={14} /> Retour aux paiements
        </Link>
      </div>
    );
  }

  const meta = paymentStateMeta[payment.state];
  const StateIcon = meta.icon;
  const events = payment.events ?? [];
  const counts: Partial<Record<TabId, number>> = {
    timeline: events.length,
    webhooks: webhooks.length,
  };
  const tabs = TAB_IDS.map((id) => ({
    id,
    label: TAB_LABELS[id],
    badge:
      TAB_WITH_COUNTER.includes(id) && counts[id] !== undefined ? (
        <span className="text-xs text-zinc-400">{counts[id]}</span>
      ) : undefined,
  }));

  return (
    <div className="mx-auto max-w-6xl px-6 py-6">
      <Link
        to="/"
        className="mb-4 inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-brand-600 dark:text-zinc-400 dark:hover:text-brand-400"
      >
        <ArrowLeft size={14} /> Retour aux paiements
      </Link>

      <div className="mb-6 flex flex-wrap items-baseline gap-x-4 gap-y-2">
        <div className="flex items-center gap-2 font-mono text-sm text-zinc-500 dark:text-zinc-400">
          <span className="truncate">{payment.uuid}</span>
          <CopyButton value={payment.uuid} />
        </div>
        <div className="ml-auto flex items-center gap-3">
          <Badge tone={meta.tone} icon={<StateIcon size={12} />}>
            {meta.label}
          </Badge>
          <Button
            variant="ghost"
            size="sm"
            leftIcon={<Trash2 size={14} />}
            onClick={() => setDeleteOpen(true)}
          >
            Supprimer
          </Button>
        </div>
      </div>

      <div className="mb-6 flex items-baseline gap-3">
        <span className="font-mono text-3xl font-semibold tabular text-zinc-900 dark:text-zinc-100">
          {formatAmount(payment.amount)}
        </span>
        <span className="text-lg text-zinc-500 dark:text-zinc-500">{payment.currency}</span>
        <span className="ml-2 text-sm text-zinc-500 dark:text-zinc-400">
          {payment.orderId} · créé le {formatShort(payment.createdAt)}
        </span>
      </div>

      <Tabs tabs={tabs} active={tab} onChange={(id) => setTab(id as TabId)} className="mb-4" />

      {tab === 'overview' && <PaymentOverview payment={{ ...payment, events }} />}
      {tab === 'timeline' && <PaymentTimeline events={events} />}
      {tab === 'webhooks' && <PaymentWebhooks webhooks={webhooks} />}
      {tab === 'payload' && <PaymentPayload webhook={webhooks[0]} />}

      <ConfirmDialog
        open={deleteOpen}
        danger
        title="Supprimer ce paiement ?"
        description={
          <>
            Le paiement <strong>{payment.orderId}</strong> et ses événements seront
            supprimés. Cette action est irréversible.
          </>
        }
        confirmLabel="Supprimer"
        loading={deleting}
        onConfirm={handleDelete}
        onCancel={() => setDeleteOpen(false)}
      />
    </div>
  );
}
