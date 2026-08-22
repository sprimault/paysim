// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Card } from '@/shared/ui/Card';
import { Skeleton } from '@/shared/ui/Skeleton';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { useT } from '@/shared/i18n/useT';
import { fetchPaymentsByToken } from '@/entities/payment/api/paymentApi';
import { fetchSubscriptionsByToken } from '@/entities/subscription/api/subscriptionApi';
import type { PaymentSummary, SubscriptionOutput } from '@/shared/model';

import { SectionTitle } from '@/shared/ui/FicheField';
/**
 * PaymentMethodUsage — ce qui a été fait avec un moyen enregistré :
 * les paiements qui l'ont enrôlé ou débité, les abonnements qui le
 * prélèvent.
 *
 * C'est la lecture inverse de la relation que PayZen porte sur la
 * transaction. Sans elle, un alias était un cul-de-sac : on voyait la
 * carte, jamais ce qu'elle avait payé.
 *
 * Les deux collections sont chargées ici plutôt que par le store
 * partagé : ce sont des vues filtrées, propres à cet écran, et les
 * mélanger au cache global ferait passer une liste partielle pour la
 * liste complète.
 */
export function PaymentMethodUsage({
  token,
  createdAt,
}: {
  token: string;
  /** Date de création de l'alias — sert à reconnaître l'enrôlement. */
  createdAt: string;
}) {
  const t = useT();
  const [payments, setPayments] = useState<PaymentSummary[]>([]);
  const [subscriptions, setSubscriptions] = useState<SubscriptionOutput[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    Promise.all([
      fetchPaymentsByToken(token, controller.signal),
      fetchSubscriptionsByToken(token, controller.signal),
    ])
      .then(([p, s]) => {
        setPayments(p);
        setSubscriptions(s);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if ((e as { name?: string }).name === 'AbortError') return;
        // Un échec de lecture ne doit pas masquer la fiche du moyen :
        // on affiche un bloc vide plutôt qu'une erreur pleine page.
        setLoading(false);
      });
    return () => controller.abort();
  }, [token]);

  const origine = trouverEnrolement(payments, createdAt);

  if (loading) {
    return (
      <Card padded className="mt-4">
        <SectionTitle>{t('paymentMethod.usage.title')}</SectionTitle>
        <Skeleton count={2} />
      </Card>
    );
  }

  if (payments.length === 0 && subscriptions.length === 0) {
    return (
      <Card padded className="mt-4">
        <SectionTitle>{t('paymentMethod.usage.title')}</SectionTitle>
        <p className="text-sm text-zinc-500 dark:text-zinc-400">
          {t('paymentMethod.usage.empty')}
        </p>
      </Card>
    );
  }

  return (
    <Card padded className="mt-4">
      <SectionTitle>{t('paymentMethod.usage.title')}</SectionTitle>

      {payments.length > 0 && (
        <section className="mb-5">
          <h4 className="mb-2 text-xs font-medium text-zinc-600 dark:text-zinc-300">
            {t('paymentMethod.usage.payments', { count: payments.length })}
          </h4>
          <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {payments.map((p) => (
              <li key={p.uuid}>
                <Link
                  to={`/payments/${p.uuid}`}
                  className="flex items-center gap-3 py-2 text-sm transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/50"
                >
                  <span className="w-24 shrink-0 font-mono text-xs tabular-nums text-zinc-900 dark:text-zinc-100">
                    {formatAmount(p.amount)} {p.currency}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-zinc-700 dark:text-zinc-300">
                    {p.orderId}
                  </span>
                  {p.uuid === origine && (
                    <Badge tone="neutral">{t('paymentMethod.usage.origin')}</Badge>
                  )}
                  <span className="shrink-0 text-xs text-zinc-500 dark:text-zinc-400">
                    {p.state}
                  </span>
                  {/* Pas d'infobulle : elle répéterait mot pour mot la
                      date déjà affichée. */}
                  <span className="w-32 shrink-0 text-right text-xs text-zinc-500 dark:text-zinc-400">
                    {formatShort(p.createdAt)}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      {subscriptions.length > 0 && (
        <section>
          <h4 className="mb-2 text-xs font-medium text-zinc-600 dark:text-zinc-300">
            {t('paymentMethod.usage.subscriptions', { count: subscriptions.length })}
          </h4>
          <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
            {subscriptions.map((s) => (
              <li key={s.id}>
                <Link
                  to={`/subscriptions/${s.id}`}
                  className="flex items-center gap-3 py-2 text-sm transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/50"
                >
                  <span className="w-24 shrink-0 font-mono text-xs tabular-nums text-zinc-900 dark:text-zinc-100">
                    {formatAmount(s.amount)} {s.currency}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-zinc-700 dark:text-zinc-300">
                    {s.orderId || s.id}
                  </span>
                  {s.cancelled ? (
                    <Badge tone="unpaid">{t('subscription.state.cancelled')}</Badge>
                  ) : (
                    <Badge tone="paid">{t('subscription.state.active')}</Badge>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </Card>
  );
}

/**
 * Reconnaît le paiement qui a créé l'alias — la question qu'on se pose
 * en débogage : « d'où vient ce token ? ».
 *
 * L'alias naît avec son premier paiement, donc c'est le plus ancien. La
 * comparaison avec sa date de création écarte le cas où ce paiement a
 * été supprimé : le plus ancien des survivants serait alors postérieur
 * à l'alias, et le marquer serait faux.
 */
function trouverEnrolement(payments: PaymentSummary[], createdAt: string): string | undefined {
  let plusAncien: PaymentSummary | undefined;
  for (const p of payments) {
    if (!plusAncien || p.createdAt < plusAncien.createdAt) plusAncien = p;
  }
  if (!plusAncien || plusAncien.createdAt > createdAt) return undefined;
  return plusAncien.uuid;
}
