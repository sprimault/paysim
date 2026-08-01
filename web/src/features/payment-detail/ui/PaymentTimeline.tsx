// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Card } from '@/shared/ui/Card';
import { formatAmount } from '@/shared/lib/numbers';
import { formatShort } from '@/shared/lib/dates';
import { eventKindMeta } from '@/shared/lib/statusMeta';
import type { EventEntry } from '@/shared/model';

interface PaymentTimelineProps {
  events: EventEntry[];
}

/**
 * Chronologie du paiement — journal d'événements. Rendue en liste
 * verticale avec un rail à gauche : icône dans un cercle + ligne
 * pointillée qui relie les événements. Dense mais lisible.
 */
export function PaymentTimeline({ events }: PaymentTimelineProps) {
  return (
    <Card padded>
      <ol className="relative">
        {events.map((e, i) => {
          const meta = eventKindMeta[e.kind];
          const Icon = meta.icon;
          const isLast = i === events.length - 1;
          return (
            <li key={i} className="relative flex gap-3 pb-4 last:pb-0">
              {!isLast && (
                <span
                  className="absolute left-3 top-6 bottom-0 w-px bg-zinc-200 dark:bg-zinc-800"
                  aria-hidden
                />
              )}
              <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-zinc-200 bg-white text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-400">
                <Icon size={12} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-4">
                  <span className="text-sm font-medium text-zinc-900 dark:text-zinc-100">
                    {meta.label}
                  </span>
                  <time
                    className="font-mono text-xs text-zinc-500 dark:text-zinc-500"
                    dateTime={e.at}
                  >
                    {formatShort(e.at)}
                  </time>
                </div>
                {(e.amount || e.note) && (
                  <div className="mt-0.5 text-xs text-zinc-600 dark:text-zinc-400">
                    {e.amount ? (
                      <span className="font-mono tabular">
                        {formatAmount(e.amount)}
                      </span>
                    ) : null}
                    {e.amount && e.note ? ' · ' : ''}
                    {e.note}
                  </div>
                )}
              </div>
            </li>
          );
        })}
      </ol>
    </Card>
  );
}
