// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Play, RefreshCw } from 'lucide-react';
import { Link } from 'react-router';
import { Button } from '@/shared/ui/Button';
import { Card } from '@/shared/ui/Card';
import { CopyButton } from '@/shared/ui/CopyButton';
import { PaymentCustomer } from './PaymentCustomer';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { truncate } from '@/shared/lib/strings';
import { isTerminal } from '@/shared/model';
import { toast } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n/useT';
import { simulatePayment } from '@/entities/payment/api/paymentApi';
import type { PaymentInStore } from '@/entities/payment/model/paymentStore';

/**
 * PaymentOverview — grille infos + panneau d'actions. Les actions
 * appellent l'API réelle. La mise à jour du store est déclenchée
 * indirectement via l'event SSE payment_state_changed qui se
 * propage par usePaysimEvents dans App.
 */
export function PaymentOverview({ payment }: { payment: PaymentInStore }) {
  const t = useT();
  const terminal = isTerminal(payment.state);
  const [pending, setPending] = useState<string | null>(null);

  async function simulate(outcome: 'PAID' | 'UNPAID') {
    setPending(outcome);
    try {
      await simulatePayment(payment.uuid, { outcome });
      toast.success(
        t('payment.detail.overview.toast.simulateSuccess', { outcome }),
        t('payment.detail.overview.toast.simulateSuccessHint'),
      );
    } catch (e) {
      toast.error(
        t('payment.detail.overview.toast.simulateError', { outcome }),
        (e as Error).message,
      );
    } finally {
      setPending(null);
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card padded className="lg:col-span-2">
        <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
          {t('payment.detail.overview.info')}
        </h3>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
          <Info label={t('payment.detail.overview.fieldState')} value={payment.state} mono />
          <Info label={t('payment.detail.overview.fieldCurrency')} value={payment.currency} />
          {/*
            Le motif bancaire en clair, code et libelle : sur la fiche on
            a la place, et c'est lui qui dit quoi faire ensuite — relancer
            plus tard sur un 51, reclamer une autre carte sur un 43.
            Absent des paiements non refuses, et de ceux dont le refus n'a
            pas de code bancaire (abandon, expiration).
          */}
          {payment.declineCode && (
            <Info
              label={t('payment.detail.overview.fieldDeclineReason')}
              value={`${payment.declineCode} — ${payment.declineMessage ?? ''}`.trim()}
              mono
            />
          )}
          <Info label={t('payment.detail.overview.fieldAmount')} value={`${formatAmount(payment.amount)} ${payment.currency}`} mono />
          <Info label={t('payment.detail.overview.fieldOrder')} value={payment.orderId} />
          <Info label={t('payment.detail.overview.fieldCreatedAt')} value={formatShort(payment.createdAt)} />
          <Info label={t('payment.detail.overview.fieldUpdatedAt')} value={formatShort(payment.updatedAt)} />
          {/* Le moyen enrôlé ou débité par ce paiement — la relation que
              PayZen porte sur la transaction. Absent d'un one-shot sans
              enrôlement, auquel cas la ligne ne s'affiche pas. */}
          {payment.paymentMethodToken && (
            <div className="contents">
              <dt className="text-xs text-zinc-500 dark:text-zinc-400">
                {t('payment.detail.overview.fieldPaymentMethod')}
              </dt>
              <dd className="flex items-center gap-1 text-sm">
                <Link
                  to={`/payment-methods/${payment.paymentMethodToken}`}
                  className="font-mono text-xs text-brand-600 hover:underline dark:text-brand-400"
                >
                  {truncate(payment.paymentMethodToken, 20)}
                </Link>
                <CopyButton value={payment.paymentMethodToken} className="p-0.5" />
              </dd>
            </div>
          )}
          {/* L'UUID vivait en haut de page, nu et sans étiquette : rien
              ne disait ce qu'était cette chaîne. Il rejoint les autres
              identifiants, nommé et copiable. */}
          <div className="contents">
            <dt className="text-xs text-zinc-500 dark:text-zinc-400">
              {t('payment.detail.overview.fieldUuid')}
            </dt>
            <dd className="flex items-center gap-1 text-sm">
              <span className="font-mono text-xs text-zinc-700 dark:text-zinc-300">
                {truncate(payment.uuid, 20)}
              </span>
              <CopyButton value={payment.uuid} className="p-0.5" />
            </dd>
          </div>
        </dl>
      </Card>

      <Card padded>
        <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
          {t('payment.detail.overview.actions')}
        </h3>
        {terminal ? (
          <p className="text-sm text-zinc-500 dark:text-zinc-400">
            {t('payment.detail.overview.terminal')}
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            <Button
              variant="primary"
              leftIcon={<Play size={14} />}
              loading={pending === 'PAID'}
              disabled={pending !== null}
              onClick={() => void simulate('PAID')}
            >
              {t('payment.detail.overview.simulatePaid')}
            </Button>
            <Button
              variant="ghost"
              leftIcon={<RefreshCw size={14} />}
              loading={pending === 'UNPAID'}
              disabled={pending !== null}
              onClick={() => void simulate('UNPAID')}
            >
              {t('payment.detail.overview.simulateUnpaid')}
            </Button>
          </div>
        )}
      </Card>
      <div className="lg:col-span-3">
        <PaymentCustomer customer={payment.customer} metadata={payment.metadata} />
      </div>
    </div>
  );
}

function Info({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="contents">
      <dt className="text-xs text-zinc-500 dark:text-zinc-400">{label}</dt>
      <dd
        className={
          'text-sm text-zinc-900 dark:text-zinc-100 ' + (mono ? 'font-mono' : '')
        }
      >
        {value}
      </dd>
    </div>
  );
}
