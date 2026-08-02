// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo, useState } from 'react';
import {
  fetchSubscription,
  fetchSubscriptions,
} from '@/entities/subscription/api/subscriptionApi';
import { useSubscriptionStore } from './subscriptionStore';
import type { SubscriptionOutput } from '@/shared/model';

/**
 * Hooks React pour consommer l'entité Subscription. Les composants
 * n'appellent jamais subscriptionApi directement — ils passent par
 * ces hooks pour bénéficier du store partagé et du fetch-on-mount.
 * Symétrique à usePaymentsList.
 */

interface FetchState {
  loading: boolean;
  error?: string;
}

export function useSubscriptionsList(): {
  subscriptions: SubscriptionOutput[];
  loading: boolean;
  error?: string;
  refresh: () => Promise<void>;
} {
  const record = useSubscriptionStore((s) => s.subscriptions);
  const setList = useSubscriptionStore((s) => s.setList);
  const subscriptions = useMemo(
    () =>
      Object.values(record).sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
    [record],
  );
  const [state, setState] = useState<FetchState>({
    loading: Object.keys(record).length === 0,
  });

  // Refetch systématique au mount. Le store sert de cache pour
  // l'affichage instantané ; on ne bascule en skeleton que si aucune
  // donnée n'est encore disponible (SWR-like). Nécessaire car aucun
  // event SSE ne pousse les subscriptions à ce jour.
  const refresh = async () => {
    const empty = Object.keys(useSubscriptionStore.getState().subscriptions).length === 0;
    if (empty) setState({ loading: true });
    try {
      const list = await fetchSubscriptions();
      setList(list);
      setState({ loading: false });
    } catch (e) {
      setState({ loading: false, error: (e as Error).message });
    }
  };

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return { subscriptions, loading: state.loading, error: state.error, refresh };
}

export function useSubscription(id: string | undefined): {
  subscription: SubscriptionOutput | undefined;
  loading: boolean;
  error?: string;
  refresh: () => Promise<void>;
} {
  const record = useSubscriptionStore((s) => s.subscriptions);
  const upsert = useSubscriptionStore((s) => s.upsert);
  const subscription = id ? record[id] : undefined;
  const [state, setState] = useState<FetchState>({ loading: !subscription });

  const refresh = async () => {
    if (!id) return;
    setState({ loading: true });
    try {
      const sub = await fetchSubscription(id);
      upsert(sub);
      setState({ loading: false });
    } catch (e) {
      setState({ loading: false, error: (e as Error).message });
    }
  };

  useEffect(() => {
    if (id && !subscription) {
      void refresh();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  return { subscription, loading: state.loading, error: state.error, refresh };
}
