// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { ArrowLeft, Ban } from 'lucide-react';
import { Link, useParams } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { Card } from '@/shared/ui/Card';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { CopyButton } from '@/shared/ui/CopyButton';
import { toast } from '@/shared/ui/toastStore';
import { formatShort } from '@/shared/lib/dates';
import { usePaymentMethod } from '@/entities/payment-method/model/usePaymentMethods';
import { revokePaymentMethod } from '@/entities/payment-method/api/paymentMethodApi';
import { paymentMethodStatus } from '@/entities/payment-method/lib/status';

/**
 * Vue détail d'un moyen de paiement enregistré. Une seule action :
 * révocation manuelle (irréversible côté simulateur). La vue reste
 * accessible même après révocation — utile pour vérifier que le
 * charge_token suivant échouera bien avec `PAYSIM_REVOKED_CARD`.
 */
export function PaymentMethodDetail() {
  const { token } = useParams();
  const { method, loading, error, refresh } = usePaymentMethod(token);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  if (!token) return null;
  if (loading && !method) {
    return <div className="mx-auto max-w-4xl px-6 py-6 text-sm text-zinc-500">Chargement…</div>;
  }
  if (error && !method) {
    return (
      <div className="mx-auto max-w-4xl px-6 py-6 text-sm text-rose-700 dark:text-rose-300">
        Impossible de charger le moyen de paiement : {error}
      </div>
    );
  }
  if (!method) return null;

  async function handleRevoke() {
    setBusy(true);
    try {
      await revokePaymentMethod(token!);
      toast.success('Moyen de paiement révoqué');
      await refresh();
    } catch (e) {
      toast.error('Révocation échouée', (e as Error).message);
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
        <ArrowLeft size={14} /> Retour à la liste
      </Link>

      <div className="mb-4 flex items-end justify-between">
        <div>
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">
            Moyen de paiement
          </h1>
          <div className="mt-1 flex items-center gap-2">
            <code className="font-mono text-xs text-zinc-500 dark:text-zinc-500">
              {method.token}
            </code>
            <CopyButton value={method.token} className="p-0.5" />
            {(() => {
              const s = paymentMethodStatus(method);
              if (s === 'revoked') return <Badge tone="unpaid">Révoqué</Badge>;
              if (s === 'expired') return <Badge tone="expired">Expiré</Badge>;
              return <Badge tone="paid">Actif</Badge>;
            })()}
          </div>
        </div>
        {!method.revoked && (
          <Button
            variant="danger"
            size="sm"
            leftIcon={<Ban size={14} />}
            onClick={() => setRevokeOpen(true)}
            disabled={busy}
          >
            Révoquer
          </Button>
        )}
      </div>

      <Card>
        <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
          <Field label="Provider" value={method.provider} />
          <Field label="Marque" value={method.brand || '—'} />
          <Field
            label="PAN"
            value={
              <code className="font-mono text-sm text-zinc-900 dark:text-zinc-100">
                {method.panMasked}
              </code>
            }
          />
          <Field
            label="Expiration"
            value={
              <span className="tabular">
                {String(method.expiryMonth).padStart(2, '0')}/{method.expiryYear}
              </span>
            }
          />
          <Field label="Créé le" value={formatShort(method.createdAt)} wide />
        </dl>
      </Card>

      <ConfirmDialog
        open={revokeOpen}
        danger
        title="Révoquer ce moyen de paiement ?"
        description={
          <>
            Cette action est irréversible côté simulateur. Les rejeux
            (<code>charge_token</code>) et les échéances d'abonnement
            utilisant ce token seront refusés avec le code
            <code> PAYSIM_REVOKED_CARD</code>.
          </>
        }
        confirmLabel="Révoquer"
        loading={busy}
        onConfirm={handleRevoke}
        onCancel={() => setRevokeOpen(false)}
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
