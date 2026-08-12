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
  const rejeux = payment.webhookReplayCount;
  return (
    // L'infobulle dit ce que le clic va faire, puis ce qui s'est deja
    // passe : c'est au moment de renvoyer qu'on veut savoir combien de
    // fois on l'a deja fait.
    <Tooltip
      label={`${libelle} · ${detailLivraisons(t, payment.webhookCount, rejeux)}`}
      focusExterne
    >
      <button
        type="button"
        onClick={() => void rejouer()}
        disabled={busy}
        aria-label={libelle}
        className="relative inline-flex rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 disabled:opacity-40 dark:hover:bg-zinc-800 dark:hover:text-zinc-200"
      >
        <Send size={14} className={busy ? 'animate-pulse' : ''} />
        {/* Pastille de comptage, et seulement s'il y a eu un rejeu : un
            « 0 » sur chaque ligne serait du bruit, alors que le geste
            qu'on cherche a retrouver est justement celui qu'on a deja
            fait.

            Positionnee hors du cadre du bouton, ce que rien ne rogne —
            le conteneur de la table n'a pas d'overflow-hidden, pour la
            meme raison qui rend l'en-tete collant possible. */}
        {rejeux > 0 && (
          <span
            className={
              'absolute -right-1 -top-1 flex h-3.5 min-w-[0.875rem] items-center ' +
              'justify-center rounded-full bg-brand-600 px-1 font-mono text-[9px] ' +
              'font-semibold leading-none text-white dark:bg-brand-500'
            }
          >
            {rejeux}
          </span>
        )}
      </button>
    </Tooltip>
  );
}

/**
 * Rend le détail des livraisons d'un paiement, pour l'infobulle.
 *
 * Composé de deux fragments plutôt que d'une phrase par cas : les
 * accords français de « livraison » et de « rejeu » sont indépendants,
 * et une clé par combinaison en ferait six pour dire la même chose.
 *
 * Le cas « autant de rejeux que de livraisons » existe : l'envoi
 * d'origine peut être sorti de la fenêtre retenue alors que ses rejeux
 * y sont encore.
 */
function detailLivraisons(t: ReturnType<typeof useT>, total: number, rejeux: number): string {
  if (total === 0) return t('payment.list.webhooks.tipNone');
  const livraisons =
    total === 1
      ? t('payment.list.webhooks.tipOne')
      : t('payment.list.webhooks.tipMany', { count: total });
  if (rejeux === 0) return livraisons;
  return rejeux === 1
    ? t('payment.list.webhooks.tipReplayOne', { livraisons })
    : t('payment.list.webhooks.tipReplayMany', { livraisons, replays: rejeux });
}
