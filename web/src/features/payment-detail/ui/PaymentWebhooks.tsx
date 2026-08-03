// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { RotateCcw, Send } from 'lucide-react';
import { Link } from 'react-router';
import { Badge } from '@/shared/ui/Badge';
import { Button } from '@/shared/ui/Button';
import { Card } from '@/shared/ui/Card';
import { CopyButton } from '@/shared/ui/CopyButton';
import { EmptyState } from '@/shared/ui/EmptyState';
import { formatShort, humanDuration } from '@/shared/lib/dates';
import { useFormatRelative } from '@/shared/hooks/useFormatRelative';
import { webhookStatusMeta } from '@/shared/lib/statusMeta';
import { useT } from '@/shared/i18n/useT';
import { toast } from '@/shared/ui/toastStore';
import { replayWebhook } from '@/entities/webhook/api/webhookApi';
import type { WebhookInStore } from '@/entities/webhook/model/webhookStore';

interface PaymentWebhooksProps {
  webhooks: WebhookInStore[];
}

export function PaymentWebhooks({ webhooks }: PaymentWebhooksProps) {
  const t = useT();
  const rel = useFormatRelative();
  if (webhooks.length === 0) {
    return (
      <EmptyState
        icon={Send}
        title={t('payment.detail.webhooks.emptyTitle')}
        hint={t('payment.detail.webhooks.emptyHint')}
      />
    );
  }

  async function handleReplay(id: string) {
    try {
      const { newDeliveryId } = await replayWebhook(id);
      toast.success(t('payment.detail.webhooks.toast.replaySuccess'), newDeliveryId);
    } catch (e) {
      toast.error(t('payment.detail.webhooks.toast.replayError'), (e as Error).message);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      {webhooks.map((w) => {
        const meta = webhookStatusMeta[w.status];
        const StatusIcon = meta.icon;
        const rttMs =
          new Date(w.completedAt).getTime() - new Date(w.createdAt).getTime();
        return (
          <Card key={w.id} padded>
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <Badge tone={meta.tone} icon={<StatusIcon size={12} />}>
                    {t(meta.labelKey)}
                  </Badge>
                  {w.statusCode ? (
                    <span className="font-mono text-xs text-zinc-500 dark:text-zinc-400">
                      HTTP {w.statusCode}
                    </span>
                  ) : null}
                  <span className="text-xs text-zinc-500 dark:text-zinc-500">
                    · {w.attempts === 1 ? t('common.attempts.one') : t('common.attempts.many', { count: w.attempts })}
                    {rttMs > 0 && ` · ${humanDuration(rttMs)}`}
                  </span>
                </div>
                <div className="mt-1 flex items-center gap-1 text-xs text-zinc-600 dark:text-zinc-400">
                  <code className="truncate font-mono">{w.url}</code>
                  <CopyButton value={w.url} className="p-0.5" />
                </div>
                <div className="mt-1 flex items-center gap-2 text-xs text-zinc-500 dark:text-zinc-500">
                  <span title={formatShort(w.createdAt)}>{rel(w.createdAt)}</span>
                  <span>·</span>
                  <code className="font-mono">{w.id}</code>
                  <CopyButton value={w.id} className="p-0.5" />
                </div>
                {w.errorMsg && (
                  <div className="mt-2 rounded bg-rose-50 px-2 py-1 text-xs text-rose-700 dark:bg-rose-950/40 dark:text-rose-300">
                    {w.errorMsg}
                  </div>
                )}
              </div>
              <div className="flex gap-2">
                <Link to={`/webhooks/${w.id}`}>
                  <Button variant="ghost" size="sm">
                    {t('payment.detail.webhooks.actionDetail')}
                  </Button>
                </Link>
                <Button
                  variant="ghost"
                  size="sm"
                  leftIcon={<RotateCcw size={14} />}
                  onClick={() => void handleReplay(w.id)}
                >
                  {t('payment.detail.webhooks.actionReplay')}
                </Button>
              </div>
            </div>
          </Card>
        );
      })}
    </div>
  );
}
