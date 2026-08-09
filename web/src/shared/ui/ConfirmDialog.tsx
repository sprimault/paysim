// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { AlertTriangle, X } from 'lucide-react';
import { useT } from '@/shared/i18n/useT';
import { useAnchoredPosition, type AnchorElement } from '@/shared/hooks/useAnchoredPosition';
import { Button } from './Button';

/**
 * Délai avant que « Confirmer » devienne actif.
 *
 * Contrepartie de l'ancrage : la boîte s'ouvre sous le curseur qui vient
 * de cliquer, donc son bouton de validation se retrouve à quelques
 * pixels du point de clic. Un geste vif ou un double-clic déclencherait
 * l'action avant que l'œil ait lu la question — sur « vider tous les
 * paiements », c'est une purge silencieuse.
 *
 * Cent cinquante millisecondes suffisent à absorber le relâchement du
 * clic initial sans se faire remarquer.
 */
const delaiActivationMs = 150;

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean; // rend le bouton confirm en variant danger
  onConfirm: () => void;
  onCancel: () => void;
  loading?: boolean;
  /**
   * Élément qui a ouvert la boîte, capturé au clic. Fourni, la boîte
   * s'affiche sous lui ; absent, elle est centrée. Le repli au centre
   * vaut aussi quand il n'y a la place ni dessous ni dessus.
   */
  anchorEl?: AnchorElement;
}

/**
 * ConfirmDialog — modal simple pour les actions destructives ou
 * irréversibles. Ferme sur Escape ou clic hors du contenu. Focus
 * management minimaliste : le bouton de confirmation reçoit le focus
 * à l'ouverture pour permettre un flow clavier (Entrée = valider,
 * Escape = annuler).
 *
 * Ancrée au déclencheur quand `anchorEl` est fourni : la confirmation
 * paraît là où le geste a eu lieu, sans traversée du regard. Le prix de
 * cette proximité est le délai d'activation ci-dessus.
 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel,
  danger = false,
  onConfirm,
  onCancel,
  loading = false,
  anchorEl,
}: ConfirmDialogProps) {
  const t = useT();
  const confirmText = confirmLabel ?? t('common.action.confirm');
  const cancelText = cancelLabel ?? t('common.action.cancel');
  const boxRef = useRef<HTMLDivElement>(null);
  const position = useAnchoredPosition(anchorEl, boxRef, open);
  const [armé, setArmé] = useState(false);

  // Ferme sur Escape.
  useEffect(() => {
    if (!open) return;
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && !loading) onCancel();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open, loading, onCancel]);

  // Réarmé à chaque ouverture : rouvrir la boîte doit redonner le même
  // délai, sans quoi la protection ne vaudrait que la première fois.
  useEffect(() => {
    if (!open) {
      setArmé(false);
      return;
    }
    const timer = setTimeout(() => setArmé(true), delaiActivationMs);
    return () => clearTimeout(timer);
  }, [open]);

  if (!open) return null;

  const ancré = position !== null;

  // Rendue dans document.body plutôt qu'à l'endroit où elle est
  // déclarée. Un `position: fixed` cesse de se caler sur la fenêtre dès
  // qu'un ancêtre porte transform, filter ou backdrop-filter : la boîte
  // se centre alors sur cet ancêtre, et paraît décalée. Sortir du sous-
  // arbre supprime la classe entière du problème plutôt que le cas du
  // jour, et vaut pour les cinq écrans qui partagent cette modale.
  return createPortal(
    <div
      // dvh et non vh : sur mobile et fenêtre réduite, vh compte les
      // barres du navigateur et déborde de la zone réellement visible.
      //
      // Le voile reste plein écran même ancré : il intercepte le clic
      // extérieur et signale que l'action est en attente.
      className={
        'fixed inset-0 z-50 h-dvh w-screen bg-black/40 ' +
        (ancré ? '' : 'flex items-center justify-center p-4')
      }
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-title"
      onClick={loading ? undefined : onCancel}
    >
      {/*
        La hauteur est bornée à la fenêtre : sans cela, un contenu long
        déborde en haut comme en bas — la boîte est centrée, donc elle
        sort des deux côtés — et rien ne permet d'atteindre les boutons.
        Le débordement défile à l'intérieur plutôt que de pousser la
        page.
      */}
      <div
        ref={boxRef}
        style={
          ancré
            ? { position: 'fixed', top: position.top, left: position.left }
            : undefined
        }
        className={
          'flex max-h-[calc(100dvh-2rem)] flex-col overflow-y-auto rounded-panel border border-zinc-200 bg-white p-5 shadow-panel dark:border-zinc-800 dark:bg-zinc-900 ' +
          (ancré ? 'w-[22rem] max-w-[calc(100vw-1rem)]' : 'w-full max-w-md')
        }
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-start gap-3">
          <div
            className={
              'rounded-full p-2 ' +
              (danger
                ? 'bg-rose-100 text-rose-700 dark:bg-rose-950/60 dark:text-rose-300'
                : 'bg-brand-100 text-brand-700 dark:bg-brand-950/60 dark:text-brand-300')
            }
          >
            <AlertTriangle size={18} />
          </div>
          <div className="flex-1">
            <h3 id="confirm-title" className="text-base font-semibold text-zinc-900 dark:text-zinc-100">
              {title}
            </h3>
          </div>
          <button
            type="button"
            onClick={onCancel}
            disabled={loading}
            aria-label={t('common.action.close')}
            className="rounded p-1 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200 disabled:opacity-50"
          >
            <X size={16} />
          </button>
        </div>

        <div className="mb-4 text-sm text-zinc-600 dark:text-zinc-400">
          {description}
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            {cancelText}
          </Button>
          {/*
            Désactivé le temps du délai d'armement. Le bouton reste à sa
            place et garde sa taille — le faire apparaître déplacerait la
            cible sous le curseur, ce qui produirait le clic accidentel
            qu'on cherche justement à éviter.
          */}
          <Button
            variant={danger ? 'danger' : 'primary'}
            onClick={onConfirm}
            loading={loading}
            disabled={!armé}
            autoFocus
          >
            {confirmText}
          </Button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
