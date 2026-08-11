// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { ChevronRight, Trash2 } from 'lucide-react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { CopyButton } from '@/shared/ui/CopyButton';
import type { Column } from '@/shared/ui/DataTable';
import { Tooltip } from '@/shared/ui/Tooltip';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { useFormatRelative } from '@/shared/hooks/useFormatRelative';
import { truncate } from '@/shared/lib/strings';
import { paymentStateMeta } from '@/shared/lib/statusMeta';
import { useT } from '@/shared/i18n/useT';
import type { PaymentSummary } from '@/shared/model';

interface Options {
  /**
   * Reçoit aussi le bouton cliqué : la confirmation s'ancre dessous, et
   * le déclencheur vit dans la cellule alors que la boîte est ouverte
   * par l'écran parent.
   */
  onDelete?: (payment: PaymentSummary, trigger: HTMLElement) => void;
  /**
   * Affiche la colonne provider quand true — utile dans l'onglet
   * « Tous » où le provider n'est pas déjà porté par le contexte de
   * l'onglet. Les onglets filtrés le laissent implicite pour ne pas
   * dupliquer une information redondante.
   */
  showProvider?: boolean;
}

/**
 * Colonnes de la liste des paiements, pour DataTable.
 *
 * Écrit comme un hook et non comme une constante : les en-têtes sont
 * traduits et les dates rendues en relatif, deux choses qui dépendent
 * de la langue courante. Les recalculer à chaque rendu est ce qui fait
 * suivre la table au changement de langue.
 *
 * Les colonnes de tri sont choisies, pas systématiques : trier sur un
 * UUID ou un alias n'apprend rien, alors que trier sur le montant ou
 * sur la date de mise à jour est le geste courant quand on cherche
 * quelle transaction a bougé en dernier.
 */
export function usePaymentColumns({ onDelete, showProvider }: Options): Column<PaymentSummary>[] {
  const t = useT();
  const rel = useFormatRelative();

  const colonnes: Column<PaymentSummary>[] = [
    {
      header: t('payment.list.column.state'),
      sortValue: (p) => p.state,
      cell: (p) => {
        const meta = paymentStateMeta[p.state];
        const StateIcon = meta.icon;
        return (
          // L'infobulle est portée par la cellule entière, pas par le
          // seul badge du code : viser deux caractères à la souris
          // demande une précision que personne n'a envie de fournir
          // pour lire un libellé. Survoler « Refusé 43 » suffit.
          //
          // Sans motif — un abandon, une expiration — Tooltip ne rend
          // qu'un passe-plat : ni curseur ni boîte n'annoncent une
          // lecture qui n'existe pas.
          <Tooltip label={p.declineMessage}>
            <Badge tone={meta.tone} icon={<StateIcon size={12} />}>
              {t(meta.labelKey)}
            </Badge>
            {/*
              Le code du motif accolé à l'état, pas son libellé : « refusé »
              ne dit pas si l'on peut reconduire, alors qu'un 51 se retente
              et qu'un 43 impose de réclamer une autre carte. Le libellé
              tient dans l'infobulle — en colonne, il pousserait le tableau.

              Rien ne s'affiche quand le refus n'a pas de motif bancaire :
              un abandon ou une expiration n'ont pas de code, et un badge
              vide vaudrait moins que l'absence de badge.
            */}
            {p.declineCode && (
              <span className="rounded bg-rose-100 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-rose-700 dark:bg-rose-950/60 dark:text-rose-300">
                {p.declineCode}
              </span>
            )}
          </Tooltip>
        );
      },
    },
  ];

  if (showProvider) {
    colonnes.push({
      header: t('payment.list.column.provider'),
      sortValue: (p) => p.provider,
      cell: (p) => (
        <span className="text-xs text-zinc-500 dark:text-zinc-400">{p.provider}</span>
      ),
    });
  }

  colonnes.push(
    {
      header: t('payment.list.column.amount'),
      align: 'right',
      sortValue: (p) => p.amount,
      cell: (p) => (
        <span className="font-mono text-sm tabular text-zinc-900 dark:text-zinc-100">
          {formatAmount(p.amount)}
          <span className="ml-1 text-xs text-zinc-500 dark:text-zinc-500">{p.currency}</span>
        </span>
      ),
    },
    {
      header: t('payment.list.column.order'),
      sortValue: (p) => p.orderId,
      cell: (p) => (
        <span className="text-sm text-zinc-700 dark:text-zinc-300">{p.orderId}</span>
      ),
    },
    {
      header: t('payment.list.column.uuid'),
      cell: (p) => (
        <div className="flex items-center gap-1">
          <Link
            to={`/payments/${p.uuid}`}
            className="font-mono text-xs text-brand-600 hover:underline dark:text-brand-400"
          >
            {truncate(p.uuid, 13)}
          </Link>
          <CopyButton value={p.uuid} className="p-0.5" />
        </div>
      ),
    },
    {
      header: t('payment.list.column.paymentMethod'),
      cell: (p) =>
        p.paymentMethodToken ? (
          <div className="flex items-center gap-1">
            <Link
              to={`/payment-methods/${p.paymentMethodToken}`}
              className="font-mono text-xs text-brand-600 hover:underline dark:text-brand-400"
            >
              {truncate(p.paymentMethodToken, 13)}
            </Link>
            <CopyButton value={p.paymentMethodToken} className="p-0.5" />
          </div>
        ) : (
          <span className="text-xs text-zinc-300 dark:text-zinc-700">—</span>
        ),
    },
    {
      header: t('payment.list.column.created'),
      sortValue: (p) => p.createdAt,
      cell: (p) => (
        <Tooltip label={formatShort(p.createdAt)}>
          <span className="text-xs text-zinc-500 dark:text-zinc-400">{rel(p.createdAt)}</span>
        </Tooltip>
      ),
    },
    {
      header: t('payment.list.column.updated'),
      sortValue: (p) => p.updatedAt,
      cell: (p) => (
        <Tooltip label={formatShort(p.updatedAt)}>
          <span className="text-xs text-zinc-500 dark:text-zinc-400">{rel(p.updatedAt)}</span>
        </Tooltip>
      ),
    },
    {
      header: t('payment.list.column.actions'),
      srOnly: true,
      align: 'right',
      cell: (p) => (
        <div className="inline-flex items-center gap-0.5">
          {onDelete && (
            <Tooltip label={t('common.action.delete')} enfantFocusable>
              <button
                type="button"
                onClick={(e) => onDelete(p, e.currentTarget)}
                aria-label={t('payment.list.action.deletePayment')}
                className="rounded p-1 text-zinc-400 hover:bg-rose-50 hover:text-rose-600 dark:hover:bg-rose-950/40 dark:hover:text-rose-400"
              >
                <Trash2 size={14} />
              </button>
            </Tooltip>
          )}
          {/* Le chevron seul n'apprend rien : l'aria-label sert les
              lecteurs d'écran, l'infobulle sert tous les autres. */}
          <Tooltip label={t('payment.list.action.openPayment')} enfantFocusable>
            <Link
              to={`/payments/${p.uuid}`}
              className="rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
              aria-label={t('payment.list.action.openPayment')}
            >
              <ChevronRight size={16} />
            </Link>
          </Tooltip>
        </div>
      ),
    },
  );

  return colonnes;
}
