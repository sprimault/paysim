// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { Check, Terminal } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { toast } from '@/shared/ui/toastStore';
import { copyToClipboard } from '@/shared/lib/clipboard';
import { useT } from '@/shared/i18n/useT';
import { fetchPayment } from '@/entities/payment/api/paymentApi';
import { buildReplayCurl } from '@/entities/payment/lib/curlCommand';

/**
 * Copie la commande `curl` qui rejoue un paiement.
 *
 * Le détail est demandé au clic, y compris depuis la fiche qui l'a déjà
 * : le sommaire de la liste ne porte ni `customer` ni `metadata`, et
 * bâtir la commande à partir de lui donnerait, sous la même icône, un
 * rejeu plus pauvre selon l'endroit d'où on l'a copié. Un aller-retour
 * de plus au clic vaut mieux qu'une commande qui varie en silence.
 */
export function CopyCurlButton({ uuid, className = '' }: { uuid: string; className?: string }) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [copie, setCopie] = useState(false);

  async function copier() {
    if (busy) return;
    setBusy(true);
    try {
      const paiement = await fetchPayment(uuid);
      if (!(await copyToClipboard(buildReplayCurl(paiement)))) {
        toast.error(t('common.action.copyFailed'));
        return;
      }
      setCopie(true);
      setTimeout(() => setCopie(false), 1200);
    } catch (e) {
      toast.error(t('payment.list.toast.curlError'), (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const libelle = copie ? t('common.action.copied') : t('payment.detail.copyCurl');
  return (
    <Tooltip label={libelle} focusExterne>
      <button
        type="button"
        onClick={() => void copier()}
        disabled={busy}
        aria-label={libelle}
        className={
          'rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 ' +
          'disabled:opacity-40 dark:hover:bg-zinc-800 dark:hover:text-zinc-200 ' +
          className
        }
      >
        {copie ? (
          <Check size={14} className="text-emerald-600 dark:text-emerald-400" />
        ) : (
          <Terminal size={14} />
        )}
      </button>
    </Tooltip>
  );
}
