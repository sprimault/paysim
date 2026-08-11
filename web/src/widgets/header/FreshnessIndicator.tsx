// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { humanDuration } from '@/shared/lib/dates';
import { useT } from '@/shared/i18n/useT';

interface FreshnessIndicatorProps {
  /** Instant du dernier signe de vie du flux, en millisecondes epoch. */
  lastEventAt?: number;
  /** État de la connexion SSE, qui change le sens de l'âge affiché. */
  connected: boolean;
}

/**
 * Âge du dernier signe de vie reçu du serveur, à côté du témoin SSE.
 *
 * Le témoin de connexion répond « le flux est-il ouvert ? », pas
 * « l'écran est-il à jour ? ». Les deux se séparent : une connexion peut
 * rester ouverte alors que plus rien n'arrive, et c'est précisément le
 * doute qui revient à chaque coupure — regarde-t-on des données vivantes
 * ou une photographie ?
 *
 * L'âge se recalcule chaque seconde. Le composant est isolé pour cela :
 * le battement ne redessine que ce libellé, pas tout l'en-tête.
 */
export function FreshnessIndicator({ lastEventAt, connected }: FreshnessIndicatorProps) {
  const t = useT();
  const [maintenant, setMaintenant] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setMaintenant(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // Rien reçu, pas même l'ouverture du flux : il n'y a aucun âge à
  // afficher, et un « il y a 0s » mentirait sur ce qui s'est passé.
  if (lastEventAt === undefined) return null;

  const age = Math.max(0, maintenant - lastEventAt);
  // Tronqué à la seconde : humanDuration rendrait « 342ms », une
  // précision que personne ne lit et qui change à chaque battement.
  const duree = humanDuration(Math.floor(age / 1000) * 1000);
  const libelle = age < 1000 ? t('header.freshness.now') : t('header.freshness.ago', { age: duree });

  return (
    <Tooltip
      label={
        connected
          ? t('header.freshness.titleConnected')
          : t('header.freshness.titleDisconnected', { age: duree })
      }
    >
      <span
        className={
          'hidden text-xs md:inline ' +
          (connected ? 'text-zinc-400 dark:text-zinc-500' : 'text-amber-600 dark:text-amber-500')
        }
      >
        {libelle}
      </span>
    </Tooltip>
  );
}
