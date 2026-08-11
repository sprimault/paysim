// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { AlertCircle, CheckCircle2, Info, X, XCircle } from 'lucide-react';
import { useEffect } from 'react';
import { useT } from '@/shared/i18n/useT';
import { useToastStore, type Toast, type ToastTone } from './toastStore';

const toneMeta: Record<ToastTone, { icon: typeof CheckCircle2; classes: string }> = {
  success: {
    icon: CheckCircle2,
    classes:
      'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-100',
  },
  error: {
    icon: XCircle,
    classes:
      'border-rose-200 bg-rose-50 text-rose-900 dark:border-rose-800 dark:bg-rose-950/60 dark:text-rose-100',
  },
  info: {
    icon: Info,
    classes:
      'border-sky-200 bg-sky-50 text-sky-900 dark:border-sky-800 dark:bg-sky-950/60 dark:text-sky-100',
  },
  warning: {
    icon: AlertCircle,
    classes:
      'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/60 dark:text-amber-100',
  },
};

export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts);
  const tt = useT();
  return (
    // En haut et centré, sous le bandeau de navigation : c'est là que le
    // regard revient de lui-même. En bas à droite, un toast demande de
    // savoir d'avance où regarder — sur un outil où la confirmation
    // arrive après un geste fait ailleurs dans la page, elle passe
    // inaperçue et disparaît avant d'avoir été lue.
    //
    // `top-16` dégage les 3,5 rem de l'en-tête collant, et la largeur
    // est bornée pour ne pas déborder sur une fenêtre étroite.
    <div
      className={
        'pointer-events-none fixed left-1/2 top-16 z-50 flex w-[calc(100%-2rem)] max-w-sm ' +
        '-translate-x-1/2 flex-col gap-2'
      }
      role="region"
      aria-label={tt('toast.regionLabel')}
    >
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}

function ToastItem({ toast: t }: { toast: Toast }) {
  const dismiss = useToastStore((s) => s.dismiss);
  const tt = useT();
  const { icon: Icon, classes } = toneMeta[t.tone];

  useEffect(() => {
    const timeout = setTimeout(() => dismiss(t.id), 5000);
    return () => clearTimeout(timeout);
  }, [t.id, dismiss]);

  return (
    <div
      className={
        'pointer-events-auto flex items-start gap-3 rounded-panel border p-3 shadow-panel ' +
        'animate-slide-in-top ' +
        classes
      }
    >
      <Icon size={18} className="mt-0.5 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{t.title}</div>
        {t.message && <div className="mt-0.5 text-xs opacity-80">{t.message}</div>}
      </div>
      <button
        type="button"
        onClick={() => dismiss(t.id)}
        aria-label={tt('common.action.close')}
        className="shrink-0 rounded p-0.5 opacity-60 hover:opacity-100"
      >
        <X size={14} />
      </button>
    </div>
  );
}
