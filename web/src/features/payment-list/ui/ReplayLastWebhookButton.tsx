// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Send } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { toast } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n/useT';
import { fetchWebhooksOfPayment, replayWebhook } from '@/entities/webhook/api/webhookApi';
import type { PaymentSummary } from '@/shared/model';

/**
 * Renvoie la dernière livraison d'un paiement sans quitter la liste.
 *
 * Le sommaire d'un paiement ne porte pas ses webhooks : on les demande
 * au clic, et au serveur — filtrer le store local serait plus rapide
 * mais faux, sa fenêtre ne gardant que les deux cents dernières
 * livraisons. Un paiement plus ancien s'y verrait déclaré sans
 * livraison alors que la base en a.
 *
 * Un aller-retour de plus au clic est le prix de cette justesse, et il
 * ne coûte rien : la liste est locale et le geste rare comparé au
 * défilement.
 */
export function ReplayLastWebhookButton({ payment }: { payment: PaymentSummary }) {
  const t = useT();
  const [busy, setBusy] = useState(false);

  async function rejouer() {
    if (busy) return;
    setBusy(true);
    try {
      const livraisons = await fetchWebhooksOfPayment(payment.uuid);
      if (livraisons.length === 0) {
        // Pas une erreur, une réponse : « rien n'est jamais parti » est
        // souvent ce qu'on cherchait à savoir.
        toast.info(t('payment.list.toast.replayNone'), payment.orderId);
        return;
      }
      const derniere = [...livraisons].sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0];
      const { newDeliveryId } = await replayWebhook(derniere.id);
      toast.success(t('payment.detail.webhooks.toast.replaySuccess'), newDeliveryId);
    } catch (e) {
      toast.error(t('payment.detail.webhooks.toast.replayError'), (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const libelle = t('payment.list.action.replayLastWebhook');
  return (
    <Tooltip label={libelle} focusExterne>
      <button
        type="button"
        onClick={() => void rejouer()}
        disabled={busy}
        aria-label={libelle}
        className="rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 disabled:opacity-40 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
      >
        <Send size={14} className={busy ? 'animate-pulse' : ''} />
      </button>
    </Tooltip>
  );
}
