// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { ArrowLeft, RotateCcw } from 'lucide-react';
import { Link, useParams } from 'react-router';
import { Badge } from '../../../shared/ui/Badge';
import { Button } from '../../../shared/ui/Button';
import { Card } from '../../../shared/ui/Card';
import { CopyButton } from '../../../shared/ui/CopyButton';
import { formatShort, humanDuration } from '../../../shared/lib/dates';
import { mockWebhooks } from '../../../shared/lib/mocks';
import { webhookStatusMeta } from '../../../shared/lib/statusMeta';
import { toast } from '../../../shared/ui/toastStore';

export function WebhookDetail() {
  const { id = '' } = useParams();
  const wh = mockWebhooks.find((w) => w.id === id);

  if (!wh) {
    return (
      <div className="mx-auto max-w-4xl px-6 py-16 text-center">
        <p className="text-sm text-zinc-500">Webhook introuvable : {id}</p>
        <Link
          to="/"
          className="mt-4 inline-flex items-center gap-1 text-sm text-brand-600 hover:underline"
        >
          <ArrowLeft size={14} /> Retour aux paiements
        </Link>
      </div>
    );
  }

  const meta = webhookStatusMeta[wh.status];
  const StatusIcon = meta.icon;
  const rttMs = new Date(wh.completedAt).getTime() - new Date(wh.createdAt).getTime();

  return (
    <div className="mx-auto max-w-6xl px-6 py-6">
      <Link
        to="/"
        className="mb-4 inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-brand-600 dark:text-zinc-400 dark:hover:text-brand-400"
      >
        <ArrowLeft size={14} /> Retour aux paiements
      </Link>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <Badge tone={meta.tone} icon={<StatusIcon size={12} />}>
              {meta.label}
            </Badge>
            {wh.statusCode ? (
              <span className="font-mono text-sm text-zinc-500 dark:text-zinc-400">
                HTTP {wh.statusCode}
              </span>
            ) : null}
            <span className="text-sm text-zinc-500 dark:text-zinc-500">
              · {wh.attempts} tentative{wh.attempts > 1 ? 's' : ''}
              {rttMs > 0 && ` · ${humanDuration(rttMs)}`}
            </span>
          </div>
          <div className="mt-2 flex items-center gap-1 font-mono text-sm text-zinc-600 dark:text-zinc-400">
            <span>{wh.id}</span>
            <CopyButton value={wh.id} className="p-0.5" />
          </div>
        </div>
        <Button
          variant="primary"
          leftIcon={<RotateCcw size={14} />}
          onClick={() => toast.success('Webhook rejoué', 'Câblage API en 3c.')}
        >
          Rejouer
        </Button>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card padded>
          <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
            Requête
          </h3>
          <dl className="mb-3 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-xs">
            <dt className="text-zinc-500">URL</dt>
            <dd className="flex min-w-0 items-center gap-1">
              <code className="truncate font-mono text-zinc-800 dark:text-zinc-200">
                {wh.url}
              </code>
              <CopyButton value={wh.url} className="p-0.5" />
            </dd>
            <dt className="text-zinc-500">Créé</dt>
            <dd className="text-zinc-800 dark:text-zinc-200">{formatShort(wh.createdAt)}</dd>
          </dl>
          <HeadersBlock headers={wh.headers} />
          <div className="mt-3">
            <h4 className="mb-1 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
              Corps
            </h4>
            <BodyBlock body={wh.body} />
          </div>
        </Card>

        <Card padded>
          <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
            Réponse
          </h3>
          {wh.status === 'pending' ? (
            <p className="text-sm text-zinc-500">En attente d'envoi…</p>
          ) : (
            <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-xs">
              <dt className="text-zinc-500">Statut HTTP</dt>
              <dd className="font-mono text-zinc-800 dark:text-zinc-200">
                {wh.statusCode || '—'}
              </dd>
              <dt className="text-zinc-500">Reçue</dt>
              <dd className="text-zinc-800 dark:text-zinc-200">
                {formatShort(wh.completedAt)}
              </dd>
              {wh.errorMsg && (
                <>
                  <dt className="text-zinc-500">Erreur</dt>
                  <dd className="text-rose-700 dark:text-rose-400">{wh.errorMsg}</dd>
                </>
              )}
            </dl>
          )}
        </Card>
      </div>
    </div>
  );
}

function HeadersBlock({ headers }: { headers: Record<string, string> }) {
  const entries = Object.entries(headers);
  if (entries.length === 0) return null;
  return (
    <div>
      <h4 className="mb-1 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
        En-têtes
      </h4>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-xs">
        {entries.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="font-mono text-zinc-500">{k}</dt>
            <dd className="break-all font-mono text-zinc-800 dark:text-zinc-200">{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function BodyBlock({ body }: { body: string }) {
  return (
    <pre className="overflow-auto rounded border border-zinc-200 bg-zinc-50 p-2 font-mono text-xs text-zinc-800 dark:border-zinc-800 dark:bg-zinc-950 dark:text-zinc-200">
      <code>{body}</code>
    </pre>
  );
}
