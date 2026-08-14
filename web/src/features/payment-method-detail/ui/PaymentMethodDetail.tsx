// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { ArrowLeft, Ban } from 'lucide-react';
import { Link, useParams } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { Card } from '@/shared/ui/Card';
import { PaymentMethodUsage } from './PaymentMethodUsage';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { CopyButton } from '@/shared/ui/CopyButton';
import { ErrorBanner } from '@/shared/ui/ErrorBanner';
import { toast } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n/useT';
import { formatShort } from '@/shared/lib/dates';
import { usePaymentMethod } from '@/entities/payment-method/model/usePaymentMethods';
import { revokePaymentMethod } from '@/entities/payment-method/api/paymentMethodApi';
import { paymentMethodStatus } from '@/entities/payment-method/lib/status';
import { useSimulatedNow } from '@/shared/hooks/useSimulatedNow';

/**
 * Vue détail d'un moyen de paiement enregistré. Une seule action :
 * révocation manuelle (irréversible côté simulateur). La vue reste
 * accessible même après révocation — utile pour vérifier que le
 * charge_token suivant échouera bien avec `PAYSIM_REVOKED_CARD`.
 */
export function PaymentMethodDetail() {
  const t = useT();
  const { token } = useParams();
  // Même horloge que la liste : les deux écrans doivent rendre le même
  // verdict sur la même carte.
  const maintenant = useSimulatedNow();
  const { method, loading, error, refresh } = usePaymentMethod(token);
  const [revokeOpen, setRevokeOpen] = useState(false);
  // Element cliqué : la boîte s'ancre dessous plutôt qu'au centre.
  const [ancre, setAncre] = useState<HTMLElement | null>(null);
  const [busy, setBusy] = useState(false);

  if (!token) return null;
  if (loading && !method) {
    return <div className="mx-auto max-w-4xl px-6 py-6 text-sm text-zinc-500">{t('paymentMethod.detail.loading')}</div>;
  }
  if (error && !method) {
    return (
      <ErrorBanner pleinePage message={t('paymentMethod.detail.errorPrefix', { error })} />
    );
  }
  if (!method) return null;

  async function handleRevoke() {
    setBusy(true);
    try {
      await revokePaymentMethod(token!);
      toast.success(t('paymentMethod.detail.toast.revokeSuccess'));
      await refresh();
    } catch (e) {
      toast.error(t('paymentMethod.detail.toast.revokeError'), (e as Error).message);
    } finally {
      setBusy(false);
      setRevokeOpen(false);
    }
  }

  return (
    <div className="mx-auto max-w-4xl px-6 py-6">
      <Link
        to="/payment-methods"
        className="mb-4 inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
      >
        <ArrowLeft size={14} /> {t('common.nav.backToList')}
      </Link>

      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            {t('paymentMethod.detail.title')}
          </h1>
          <div className="mt-1 flex items-center gap-2">
            <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
              {method.token}
            </code>
            <CopyButton value={method.token} className="p-0.5" />
            {(() => {
              const s = paymentMethodStatus(method, maintenant);
              if (s === 'revoked') return <Badge tone="unpaid">{t('paymentMethod.state.revoked')}</Badge>;
              if (s === 'expired') return <Badge tone="expired">{t('paymentMethod.state.expired')}</Badge>;
              return <Badge tone="paid">{t('paymentMethod.state.active')}</Badge>;
            })()}
          </div>
        </div>
        {!method.revoked && (
          <Button
            variant="danger"
            size="sm"
            leftIcon={<Ban size={14} />}
            onClick={(e) => {
              setAncre(e.currentTarget);
              setRevokeOpen(true);
            }}
            disabled={busy}
          >
            {t('paymentMethod.detail.action.revoke')}
          </Button>
        )}
      </div>

      <Card>
        <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
          <Field label={t('paymentMethod.detail.field.provider')} value={method.provider} />
          <Field label={t('paymentMethod.detail.field.brand')} value={method.brand || '—'} />
          <Field
            label={t('paymentMethod.detail.field.pan')}
            value={
              <code className="font-mono text-sm text-zinc-900 dark:text-zinc-100">
                {method.panMasked}
              </code>
            }
          />
          <Field
            label={t('paymentMethod.detail.field.expiry')}
            value={
              <span className="tabular">
                {String(method.expiryMonth).padStart(2, '0')}/{method.expiryYear}
              </span>
            }
          />
          <Field label={t('paymentMethod.detail.field.createdAt')} value={formatShort(method.createdAt)} wide />
        </dl>
      </Card>

      <PaymentMethodUsage token={method.token} createdAt={method.createdAt} />

      <ConfirmDialog
        open={revokeOpen}
        danger
        title={t('paymentMethod.detail.dialog.revokeTitle')}
        description={t('paymentMethod.detail.dialog.revokeDescription')}
        confirmLabel={t('paymentMethod.detail.dialog.revokeConfirm')}
        loading={busy}
        onConfirm={handleRevoke}
        onCancel={() => setRevokeOpen(false)}
        anchorEl={ancre}
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
