// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo, useState } from 'react';
import { useSSE } from '../../../shared/hooks/useSSE';
import { isPaysimEvent } from '../../../shared/model/events';
import { fetchWebhook, fetchWebhooks } from '../api/webhookApi';
import { useWebhookStore, type WebhookInStore } from './webhookStore';

/**
 * Hooks React pour consommer l'entité Webhook. Miroir de usePayments —
 * mêmes patterns : fetch-on-mount, refresh explicite, SSE qui
 * refetch pour garantir la cohérence.
 */

interface FetchState {
  loading: boolean;
  error?: string;
}

export function useWebhooksList(): {
  webhooks: WebhookInStore[];
  loading: boolean;
  error?: string;
  refresh: () => Promise<void>;
} {
  // Même stratégie que usePaymentsList : lecture du record brut +
  // tri via useMemo pour éviter la boucle de rendu.
  const webhooksRecord = useWebhookStore((s) => s.webhooks);
  const listLoaded = useWebhookStore((s) => s.listLoaded);
  const setList = useWebhookStore((s) => s.setList);
  const webhooks = useMemo(
    () =>
      Object.values(webhooksRecord).sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
    [webhooksRecord],
  );
  const [state, setState] = useState<FetchState>({ loading: !listLoaded });

  const refresh = async () => {
    setState({ loading: true });
    try {
      const list = await fetchWebhooks();
      setList(list);
      setState({ loading: false });
    } catch (e) {
      setState({ loading: false, error: (e as Error).message });
    }
  };

  useEffect(() => {
    if (!listLoaded) {
      void refresh();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listLoaded]);

  return { webhooks, loading: state.loading, error: state.error, refresh };
}

export function useWebhook(id: string): {
  webhook: WebhookInStore | undefined;
  loading: boolean;
  error?: string;
} {
  const webhook = useWebhookStore((s) => s.webhooks[id]);
  const setDetail = useWebhookStore((s) => s.setDetail);
  const [state, setState] = useState<FetchState>({ loading: !webhook?.body });

  useEffect(() => {
    if (!id) return;
    if (webhook?.body !== undefined) {
      setState({ loading: false });
      return;
    }
    const controller = new AbortController();
    setState({ loading: true });
    fetchWebhook(id, controller.signal)
      .then((d) => {
        setDetail(d);
        setState({ loading: false });
      })
      .catch((e: unknown) => {
        if ((e as { name?: string }).name === 'AbortError') return;
        setState({ loading: false, error: (e as Error).message });
      });
    return () => controller.abort();
  }, [id, webhook?.body, setDetail]);

  return { webhook, loading: state.loading, error: state.error };
}

/**
 * useWebhookEvents branche le SSE sur le store webhook. Sur chaque
 * event webhook_*, refetch la liste (le payload est trop léger pour
 * reconstruire un WebhookEntry complet et l'entrée peut apparaître
 * ou changer de statut). Refetch de la liste plutôt qu'un fetch
 * unitaire évite un cascade d'appels en cas de rafale d'events.
 */
export function useWebhookEvents(streamPath = '/paysim/api/v1/events/stream'): {
  connected: boolean;
} {
  const setList = useWebhookStore((s) => s.setList);

  return useSSE(streamPath, (raw) => {
    if (!isPaysimEvent(raw)) return;
    if (
      raw.type !== 'webhook_enqueued' &&
      raw.type !== 'webhook_delivered' &&
      raw.type !== 'webhook_failed'
    ) {
      return;
    }
    void fetchWebhooks().then(setList).catch(() => undefined);
  });
}
