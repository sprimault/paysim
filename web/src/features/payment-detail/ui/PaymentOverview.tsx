// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Play, RefreshCw } from 'lucide-react';
import { Button } from '../../../shared/ui/Button';
import { Card } from '../../../shared/ui/Card';
import { formatAmount } from '../../../shared/lib/numbers';
import { formatShort } from '../../../shared/lib/dates';
import { isTerminal } from '../../../shared/model';
import { toast } from '../../../shared/ui/toastStore';
import type { PaymentDetail } from '../../../shared/model';

/**
 * PaymentOverview — grille infos + panneau d'actions. Les actions
 * appellent l'API en 3c ; en 3b on toast pour valider l'UX.
 */
export function PaymentOverview({ payment }: { payment: PaymentDetail }) {
  const terminal = isTerminal(payment.state);

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card padded className="lg:col-span-2">
        <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
          Informations
        </h3>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
          <Info label="État" value={payment.state} mono />
          <Info label="Devise" value={payment.currency} />
          <Info label="Montant" value={`${formatAmount(payment.amount)} ${payment.currency}`} mono />
          <Info label="Commande" value={payment.orderId} />
          <Info label="Créé le" value={formatShort(payment.createdAt)} />
          <Info label="Mis à jour" value={formatShort(payment.updatedAt)} />
        </dl>
      </Card>

      <Card padded>
        <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
          Actions
        </h3>
        {terminal ? (
          <p className="text-sm text-zinc-500 dark:text-zinc-400">
            État terminal — aucune action possible.
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            <Button
              variant="primary"
              leftIcon={<Play size={14} />}
              onClick={() => toast.success('Simulation demandée', 'Câblage API en 3c.')}
            >
              Simuler PAID
            </Button>
            <Button
              variant="ghost"
              leftIcon={<RefreshCw size={14} />}
              onClick={() => toast.info('Simulation UNPAID', 'Câblage API en 3c.')}
            >
              Simuler UNPAID
            </Button>
          </div>
        )}
      </Card>
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
