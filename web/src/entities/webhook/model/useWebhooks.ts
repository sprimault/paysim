// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo, useState } from 'react';
import {
  fetchWebhook,
  fetchWebhooks,
  fetchWebhooksOfPayment,
} from '@/entities/webhook/api/webhookApi';
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

/**
 * useWebhooksOfPayment ne remonte que les livraisons d'un paiement,
 * plus récente d'abord.
 *
 * Les entrées passent par `upsert` et non `setList` : celui-ci
 * remplace tout le store, une liste filtrée y effacerait donc les
 * webhooks des autres paiements. Les identifiants du paiement sont
 * gardés localement, le store restant la source des détails déjà
 * chargés.
 */
export function useWebhooksOfPayment(paymentUuid: string): {
  webhooks: WebhookInStore[];
  loading: boolean;
  error?: string;
  refresh: () => void;
} {
  const webhooksRecord = useWebhookStore((s) => s.webhooks);
  const upsert = useWebhookStore((s) => s.upsert);
  const [ids, setIds] = useState<string[]>([]);
  const [state, setState] = useState<FetchState>({ loading: Boolean(paymentUuid) });
  // Incrémenté par refresh : sert de dépendance à l'effet, pour rejouer
  // le fetch sans dupliquer sa logique hors de l'effet.
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!paymentUuid) {
      setIds([]);
      setState({ loading: false });
      return;
    }
    const controller = new AbortController();
    setState({ loading: true });
    fetchWebhooksOfPayment(paymentUuid, controller.signal)
      .then((list) => {
        list.forEach(upsert);
        setIds(list.map((w) => w.id));
        setState({ loading: false });
      })
      .catch((e: unknown) => {
        if ((e as { name?: string }).name === 'AbortError') return;
        setState({ loading: false, error: (e as Error).message });
      });
    return () => controller.abort();
  }, [paymentUuid, upsert, tick]);

  const webhooks = useMemo(
    () =>
      ids
        .map((id) => webhooksRecord[id])
        .filter((w): w is WebhookInStore => w !== undefined)
        .sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
    [ids, webhooksRecord],
  );

  return {
    webhooks,
    loading: state.loading,
    error: state.error,
    refresh: () => setTick((n) => n + 1),
  };
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
