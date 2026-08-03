// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { ArrowLeft, RotateCcw } from 'lucide-react';
import { Link, useParams } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { Card } from '@/shared/ui/Card';
import { CopyButton } from '@/shared/ui/CopyButton';
import { Skeleton } from '@/shared/ui/Skeleton';
import { formatShort, humanDuration } from '@/shared/lib/dates';
import { webhookStatusMeta } from '@/shared/lib/statusMeta';
import { useT } from '@/shared/i18n/useT';
import { toast } from '@/shared/ui/toastStore';
import { replayWebhook } from '@/entities/webhook/api/webhookApi';
import { useWebhook } from '@/entities/webhook/model/useWebhooks';

export function WebhookDetail() {
  const t = useT();
  const { id = '' } = useParams();
  const { webhook: wh, loading, error } = useWebhook(id);
  const [replaying, setReplaying] = useState(false);

  async function handleReplay() {
    if (!wh) return;
    setReplaying(true);
    try {
      const { newDeliveryId } = await replayWebhook(wh.id);
      toast.success(t('payment.detail.webhooks.toast.replaySuccess'), newDeliveryId);
    } catch (e) {
      toast.error(t('payment.detail.webhooks.toast.replayError'), (e as Error).message);
    } finally {
      setReplaying(false);
    }
  }

  if (loading && !wh) {
    return (
      <div className="mx-auto max-w-6xl px-6 py-6">
        <Skeleton className="mb-3 h-4 w-32" />
        <Skeleton className="mb-6 h-6 w-96" />
        <Skeleton count={4} />
      </div>
    );
  }

  if (error || !wh) {
    return (
      <div className="mx-auto max-w-4xl px-6 py-16 text-center">
        <p className="text-sm text-zinc-500">
          {error ? t('common.error.prefix', { error }) : t('webhook.detail.notFound', { id })}
        </p>
        <Link
          to="/"
          className="mt-4 inline-flex items-center gap-1 text-sm text-brand-600 hover:underline"
        >
          <ArrowLeft size={14} /> {t('common.nav.backToPayments')}
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
        <ArrowLeft size={14} /> {t('common.nav.backToPayments')}
      </Link>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <Badge tone={meta.tone} icon={<StatusIcon size={12} />}>
              {t(meta.labelKey)}
            </Badge>
            {wh.statusCode ? (
              <span className="font-mono text-sm text-zinc-500 dark:text-zinc-400">
                HTTP {wh.statusCode}
              </span>
            ) : null}
            <span className="text-sm text-zinc-500 dark:text-zinc-500">
              · {wh.attempts === 1 ? t('common.attempts.one') : t('common.attempts.many', { count: wh.attempts })}
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
          loading={replaying}
          onClick={() => void handleReplay()}
        >
          {t('webhook.detail.actionReplay')}
        </Button>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card padded>
          <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
            {t('webhook.detail.section.request')}
          </h3>
          <dl className="mb-3 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-xs">
            <dt className="text-zinc-500">{t('webhook.detail.field.url')}</dt>
            <dd className="flex min-w-0 items-center gap-1">
              <code className="truncate font-mono text-zinc-800 dark:text-zinc-200">
                {wh.url}
              </code>
              <CopyButton value={wh.url} className="p-0.5" />
            </dd>
            <dt className="text-zinc-500">{t('webhook.detail.field.createdAt')}</dt>
            <dd className="text-zinc-800 dark:text-zinc-200">{formatShort(wh.createdAt)}</dd>
          </dl>
          {wh.headers && <HeadersBlock headers={wh.headers} headersLabel={t('webhook.detail.field.headers')} />}
          <div className="mt-3">
            <h4 className="mb-1 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
              {t('webhook.detail.field.body')}
            </h4>
            <BodyBlock body={wh.body ?? ''} />
          </div>
        </Card>

        <Card padded>
          <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
            {t('webhook.detail.section.response')}
          </h3>
          {wh.status === 'pending' ? (
            <p className="text-sm text-zinc-500">{t('webhook.detail.pendingSend')}</p>
          ) : (
            <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-xs">
              <dt className="text-zinc-500">{t('webhook.detail.field.httpStatus')}</dt>
              <dd className="font-mono text-zinc-800 dark:text-zinc-200">
                {wh.statusCode || '—'}
              </dd>
              <dt className="text-zinc-500">{t('webhook.detail.field.receivedAt')}</dt>
              <dd className="text-zinc-800 dark:text-zinc-200">
                {formatShort(wh.completedAt)}
              </dd>
              {wh.errorMsg && (
                <>
                  <dt className="text-zinc-500">{t('webhook.detail.field.error')}</dt>
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

function HeadersBlock({ headers, headersLabel }: { headers: Record<string, string>; headersLabel: string }) {
  const entries = Object.entries(headers);
  if (entries.length === 0) return null;
  return (
    <div>
      <h4 className="mb-1 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
        {headersLabel}
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
