// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo, useState } from 'react';
import {
  fetchPaymentMethod,
  fetchPaymentMethods,
} from '@/entities/payment-method/api/paymentMethodApi';
import { usePaymentMethodStore } from './paymentMethodStore';
import type { PaymentMethodOutput } from '@/shared/model';

interface FetchState {
  loading: boolean;
  error?: string;
}

/**
 * usePaymentMethodsList expose la liste triée (plus récent d'abord)
 * et déclenche un fetchPaymentMethods au premier montage si le store
 * est vide. Symétrique à useSubscriptionsList / usePaymentsList.
 */
export function usePaymentMethodsList(): {
  methods: PaymentMethodOutput[];
  loading: boolean;
  error?: string;
  refresh: () => Promise<void>;
} {
  const record = usePaymentMethodStore((s) => s.methods);
  const listLoaded = usePaymentMethodStore((s) => s.listLoaded);
  const setList = usePaymentMethodStore((s) => s.setList);
  const methods = useMemo(
    () =>
      Object.values(record).sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
    [record],
  );
  const [state, setState] = useState<FetchState>({ loading: !listLoaded });

  const refresh = async () => {
    setState({ loading: true });
    try {
      const list = await fetchPaymentMethods();
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

  return { methods, loading: state.loading, error: state.error, refresh };
}

/**
 * usePaymentMethod charge un moyen de paiement par token — utilisé
 * par la vue détail. Retombe sur un fetch dédié si l'entrée n'est
 * pas déjà dans le store (bookmark direct sur /payment-methods/:token).
 */
export function usePaymentMethod(token: string | undefined): {
  method: PaymentMethodOutput | undefined;
  loading: boolean;
  error?: string;
  refresh: () => Promise<void>;
} {
  const record = usePaymentMethodStore((s) => s.methods);
  const upsert = usePaymentMethodStore((s) => s.upsert);
  const method = token ? record[token] : undefined;
  const [state, setState] = useState<FetchState>({ loading: !method });

  const refresh = async () => {
    if (!token) return;
    setState({ loading: true });
    try {
      const m = await fetchPaymentMethod(token);
      upsert(m);
      setState({ loading: false });
    } catch (e) {
      setState({ loading: false, error: (e as Error).message });
    }
  };

  useEffect(() => {
    if (token && !method) {
      void refresh();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  return { method, loading: state.loading, error: state.error, refresh };
}
