// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Card } from '@/shared/ui/Card';
import { Skeleton } from '@/shared/ui/Skeleton';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { paymentStateMeta } from '@/shared/lib/statusMeta';
import { useT } from '@/shared/i18n/useT';
import { fetchPaymentsBySubscription } from '@/entities/payment/api/paymentApi';
import type { PaymentSummary } from '@/shared/model';

/**
 * SubscriptionBillings — les échéances produites par un abonnement.
 *
 * Lecture inverse du rattachement que Paysim porte dans les métadonnées
 * du paiement. Sans elle, un abonnement était un cul-de-sac : on voyait
 * la règle de récurrence, jamais ce qu'elle avait prélevé — ni qu'une
 * échéance avait échoué.
 *
 * Le motif de refus est affiché sur chaque ligne : c'est ici qu'il
 * compte le plus, un prélèvement récurrent refusé en 51 se reconduisant
 * alors qu'un 43 impose de réclamer une autre carte.
 *
 * Chargé localement plutôt que par le store partagé : c'est une vue
 * filtrée propre à cet écran, et la mêler au cache global ferait passer
 * une liste partielle pour la liste complète.
 */
export function SubscriptionBillings({ subscriptionId }: { subscriptionId: string }) {
  const t = useT();
  const [payments, setPayments] = useState<PaymentSummary[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    fetchPaymentsBySubscription(subscriptionId, controller.signal)
      .then((p) => {
        setPayments(p);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if ((e as { name?: string }).name === 'AbortError') return;
        // Un échec de lecture ne doit pas masquer la fiche : bloc vide
        // plutôt qu'erreur pleine page.
        setLoading(false);
      });
    return () => controller.abort();
  }, [subscriptionId]);

  if (loading) {
    return (
      <Card padded className="mt-4">
        <Titre>{t('subscription.billings.title')}</Titre>
        <Skeleton count={2} />
      </Card>
    );
  }

  if (payments.length === 0) {
    return (
      <Card padded className="mt-4">
        <Titre>{t('subscription.billings.title')}</Titre>
        <p className="text-sm text-zinc-500 dark:text-zinc-400">
          {t('subscription.billings.empty')}
        </p>
      </Card>
    );
  }

  return (
    <Card padded className="mt-4">
      <Titre>
        {payments.length === 1
          ? t('subscription.billings.one')
          : t('subscription.billings.many', { count: payments.length })}
      </Titre>
      <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
        {payments.map((p) => {
          const meta = paymentStateMeta[p.state];
          const StateIcon = meta.icon;
          return (
            <li key={p.uuid}>
              <Link
                to={`/payments/${p.uuid}`}
                className="flex items-center gap-3 py-2 text-sm transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/50"
              >
                <span className="w-24 shrink-0 font-mono text-xs tabular-nums text-zinc-900 dark:text-zinc-100">
                  {formatAmount(p.amount)} {p.currency}
                </span>
                <Badge tone={meta.tone} icon={<StateIcon size={12} />}>
                  {t(meta.labelKey)}
                </Badge>
                {p.declineCode && (
                  <span
                    title={p.declineMessage}
                    className="rounded bg-rose-100 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-rose-700 dark:bg-rose-950/60 dark:text-rose-300"
                  >
                    {p.declineCode}
                  </span>
                )}
                <span className="min-w-0 flex-1 truncate text-zinc-700 dark:text-zinc-300">
                  {p.orderId}
                </span>
                <span className="shrink-0 text-xs text-zinc-500 dark:text-zinc-400">
                  {formatShort(p.createdAt)}
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

/** Intitulé de section, aligné sur celui des autres fiches. */
function Titre({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
      {children}
    </h3>
  );
}
