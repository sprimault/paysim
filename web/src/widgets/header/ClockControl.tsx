// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import { Clock, RotateCcw } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { useT } from '@/shared/i18n/useT';
import { humanDuration } from '@/shared/lib/dates';
import { estDecalee, useClockStore } from '@/shared/model/clockStore';
import { toast } from '@/shared/ui/toastStore';

/**
 * Pilotage de l'horloge du simulateur, dans l'en-tête.
 *
 * Deux rôles, et le premier compte plus que le second. **Signaler** :
 * une instance avancée date ses paiements dans le futur, et sans
 * bandeau rien ne dit pourquoi — le simulateur mentirait sans le
 * signaler, ce qu'il existe précisément pour éviter. **Piloter**
 * ensuite : trois avances et un retour, de quoi montrer un impayé
 * différé en direct sans écrire une ligne de scénario.
 *
 * Le bandeau n'apparaît que décalage non nul : au repos, l'en-tête ne
 * gagne qu'une icône.
 */
export function ClockControl() {
  const t = useT();
  const decalageMs = useClockStore((s) => s.decalageMs);
  const rafraichir = useClockStore((s) => s.rafraichir);
  const avancer = useClockStore((s) => s.avancer);
  const reinitialiser = useClockStore((s) => s.reinitialiser);
  const [ouvert, setOuvert] = useState(false);

  // Une lecture au montage suffit : le décalage ne bouge que sur action
  // explicite, et un sondage périodique ferait du bruit réseau pour
  // observer une valeur qui ne change pas toute seule.
  useEffect(() => {
    void rafraichir().catch((err: unknown) => {
      toast.error(t('header.clock.failed'), err instanceof Error ? err.message : undefined);
    });
  }, [rafraichir, t]);

  const decalee = estDecalee(decalageMs);

  // Un échec se dit. Avaler l'erreur donnait un bouton qui ne fait
  // rien et n'explique rien — exactement le silence que ce simulateur
  // existe pour éviter.
  const agir = (action: () => Promise<void>) => () => {
    setOuvert(false);
    void action().catch((err: unknown) => {
      toast.error(t('header.clock.failed'), err instanceof Error ? err.message : undefined);
    });
  };

  return (
    <div className="relative flex items-center gap-2">
      {decalee && (
        <Tooltip label={t('header.clock.title')}>
          <span
            className={
              'flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs ' +
              'font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-200'
            }
            data-testid="clock-shifted"
          >
            {t('header.clock.shifted', { offset: humanDuration(Math.abs(decalageMs)) })}
          </span>
        </Tooltip>
      )}

      <Tooltip label={t('header.clock.advance')}>
        <button
          type="button"
          aria-label={t('header.clock.advance')}
          onClick={() => setOuvert((o) => !o)}
          className={
            'rounded p-1.5 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 ' +
            'dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100'
          }
        >
          <Clock size={16} />
        </button>
      </Tooltip>

      {ouvert && (
        <div
          className={
            'absolute right-0 top-9 z-20 flex flex-col rounded-md border border-slate-200 ' +
            'bg-white py-1 shadow-lg dark:border-slate-700 dark:bg-slate-900'
          }
        >
          {(
            [
              ['1h', 'header.clock.advanceHour'],
              ['24h', 'header.clock.advanceDay'],
              ['168h', 'header.clock.advanceWeek'],
              // Un mois n'est pas une durée Go : trente jours, ce qui
              // suffit à franchir une échéance mensuelle.
              ['720h', 'header.clock.advanceMonth'],
            ] as const
          ).map(([duree, cle]) => (
            <button
              key={duree}
              type="button"
              onClick={agir(() => avancer(duree))}
              className={
                'whitespace-nowrap px-3 py-1.5 text-left text-sm text-slate-700 ' +
                'hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800'
              }
            >
              {t(cle)}
            </button>
          ))}
          <button
            type="button"
            onClick={agir(reinitialiser)}
            disabled={!decalee}
            className={
              'flex items-center gap-1.5 whitespace-nowrap border-t border-slate-200 px-3 ' +
              'py-1.5 text-left text-sm text-slate-700 hover:bg-slate-100 disabled:opacity-40 ' +
              'dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800'
            }
          >
            <RotateCcw size={13} className="shrink-0" />
            {t('header.clock.reset')}
          </button>
        </div>
      )}
    </div>
  );
}
