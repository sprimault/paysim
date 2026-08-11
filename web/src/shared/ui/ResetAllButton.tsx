// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Trash2 } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { ConfirmDialog } from './ConfirmDialog';
import { toast } from './toastStore';
import { useT } from '@/shared/i18n/useT';
import { apiPostJson } from '@/shared/api/client';

/** Compte de ce qu'une réinitialisation a supprimé, par collection. */
interface ResetOutput {
  payments: number;
  subscriptions: number;
  paymentMethods: number;
  webhooks: number;
}

/**
 * ResetAllButton — remet le simulateur à zéro : paiements, abonnements,
 * moyens de paiement et historique des webhooks.
 *
 * Placé dans le Header parce que le besoin est transverse — on nettoie
 * entre deux campagnes, pas depuis une liste en particulier. Sa
 * proximité avec les sélecteurs de thème et de langue impose la
 * confirmation : l'action est irréversible et à portée de clic distrait.
 *
 * Le résultat est annoncé collection par collection plutôt que par un
 * « c'est fait » : savoir que 12 paiements et 4 moyens ont disparu
 * permet de vérifier qu'on a vidé ce qu'on croyait vider.
 */
export function ResetAllButton() {
  const t = useT();
  const [open, setOpen] = useState(false);
  // Element cliqué : la boîte s'ancre dessous plutôt qu'au centre.
  const [ancre, setAncre] = useState<HTMLElement | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleConfirm() {
    setLoading(true);
    try {
      const out = await apiPostJson<Record<string, never>, ResetOutput>(
        '/paysim/api/v1/reset',
        {},
      );
      toast.success(
        t('header.reset.toastTitle'),
        t('header.reset.toastDetail', {
          payments: out.payments,
          subscriptions: out.subscriptions,
          paymentMethods: out.paymentMethods,
          webhooks: out.webhooks,
        }),
      );
      setOpen(false);
      // Rechargement plutôt que rafraîchissement ciblé : la
      // réinitialisation touche toutes les vues, et l'utilisateur peut
      // se trouver sur n'importe laquelle.
      window.location.reload();
    } catch (e) {
      toast.error(t('header.reset.toastError'), (e as Error).message);
      setLoading(false);
    }
  }

  return (
    <>
      <Tooltip label={t('header.reset.title')} enfantFocusable>
        <button
          type="button"
          onClick={(e) => {
            setAncre(e.currentTarget);
            setOpen(true);
          }}
          aria-label={t('header.reset.title')}
          className="rounded p-1.5 text-zinc-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:text-zinc-400 dark:hover:bg-red-950/40 dark:hover:text-red-400"
        >
          <Trash2 size={16} strokeWidth={2} />
        </button>
      </Tooltip>
      <ConfirmDialog
        open={open}
        title={t('header.reset.dialogTitle')}
        description={t('header.reset.dialogDescription')}
        confirmLabel={t('header.reset.confirm')}
        danger
        loading={loading}
        onConfirm={handleConfirm}
        onCancel={() => setOpen(false)}
        anchorEl={ancre}
      />
    </>
  );
}
