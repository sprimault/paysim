// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useMemo } from 'react';
import { Card } from '../../../shared/ui/Card';
import { EmptyState } from '../../../shared/ui/EmptyState';
import { JsonViewer } from '../../../shared/ui/JsonViewer';
import { FileJson } from 'lucide-react';
import { parsePayzenBody } from '../../../shared/lib/payzen';
import type { WebhookDetail } from '../../../shared/model';

interface PaymentPayloadProps {
  webhook?: WebhookDetail;
}

/**
 * PaymentPayload — extrait le kr-answer d'un body PayZen form-encoded
 * et l'affiche pretty-printé. Les autres champs (kr-hash, kr-hash-key)
 * restent visibles dans une liste plate à côté.
 */
export function PaymentPayload({ webhook }: PaymentPayloadProps) {
  const parsed = useMemo(() => parsePayzenBody(webhook?.body ?? ''), [webhook?.body]);

  if (!webhook) {
    return (
      <EmptyState
        icon={FileJson}
        title="Pas de charge utile disponible"
        hint="Aucun webhook n'a encore été livré pour ce paiement."
      />
    );
  }

  return (
    <div className="grid gap-4 lg:grid-cols-5">
      <Card padded className="lg:col-span-3">
        <h3 className="mb-2 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
          kr-answer
        </h3>
        {parsed.krAnswer ? (
          <JsonViewer value={parsed.krAnswer} maxHeight="max-h-[28rem]" />
        ) : (
          <p className="text-sm text-zinc-500">Aucun champ kr-answer dans ce body.</p>
        )}
      </Card>
      <Card padded className="lg:col-span-2">
        <h3 className="mb-2 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
          Autres champs
        </h3>
        <dl className="grid gap-1 text-xs">
          {Object.entries(parsed.rest).map(([k, v]) => (
            <div key={k} className="flex flex-col gap-0.5 border-b border-zinc-100 pb-1 last:border-b-0 dark:border-zinc-800">
              <dt className="font-mono text-zinc-500 dark:text-zinc-500">{k}</dt>
              <dd className="break-all font-mono text-zinc-800 dark:text-zinc-200">{v}</dd>
            </div>
          ))}
        </dl>
      </Card>
    </div>
  );
}
