// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo, useState } from 'react';
import { fetchPayment, fetchPayments } from '@/entities/payment/api/paymentApi';
import { usePaymentStore, type PaymentInStore } from './paymentStore';

/**
 * Hooks React pour consommer l'entité Payment. Les composants
 * n'appellent jamais paymentApi directement — ils passent par ces
 * hooks pour bénéficier du store partagé et du fetch-on-mount.
 */

interface FetchState {
  loading: boolean;
  error?: string;
}

/**
 * usePaymentsList expose la liste triée (plus récent d'abord) et
 * déclenche un fetchPayments au premier montage si le store est vide.
 * `refresh()` force un refetch (utile après une reconnexion SSE
 * potentiellement lossy).
 */
export function usePaymentsList(): {
  payments: PaymentInStore[];
  loading: boolean;
  error?: string;
  refresh: () => Promise<void>;
} {
  // Selector qui retourne une référence stable : on lit le record
  // brut (référentiellement stable tant qu'aucune mutation), le tri
  // se fait via useMemo. Sans ça, Object.values(...).sort(...) dans
  // le selector produit un nouveau tableau à chaque render et
  // déclenche une boucle infinie de mise à jour.
  const paymentsRecord = usePaymentStore((s) => s.payments);
  const listLoaded = usePaymentStore((s) => s.listLoaded);
  const setList = usePaymentStore((s) => s.setList);
  const payments = useMemo(
    () =>
      Object.values(paymentsRecord).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)),
    [paymentsRecord],
  );
  const [state, setState] = useState<FetchState>({ loading: !listLoaded });

  const refresh = async () => {
    setState({ loading: true });
    try {
      const list = await fetchPayments();
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- refresh est stable au sens fonctionnel, on ne veut pas relancer à chaque render.
  }, [listLoaded]);

  return { payments, loading: state.loading, error: state.error, refresh };
}

/**
 * usePayment retourne un paiement précis. Fetch automatique si absent
 * du store ou si le détail (events) n'a pas encore été chargé.
 * `undefined` tant que la requête n'a pas abouti.
 */
export function usePayment(uuid: string): {
  payment: PaymentInStore | undefined;
  loading: boolean;
  error?: string;
} {
  const payment = usePaymentStore((s) => s.payments[uuid]);
  const setDetail = usePaymentStore((s) => s.setDetail);
  const [state, setState] = useState<FetchState>({ loading: !payment?.events });

  useEffect(() => {
    if (!uuid) return;
    if (payment?.events) {
      setState({ loading: false });
      return;
    }
    const controller = new AbortController();
    setState({ loading: true });
    fetchPayment(uuid, controller.signal)
      .then((d) => {
        setDetail(d);
        setState({ loading: false });
      })
      .catch((e: unknown) => {
        if ((e as { name?: string }).name === 'AbortError') return;
        setState({ loading: false, error: (e as Error).message });
      });
    return () => controller.abort();
  }, [uuid, payment?.events, setDetail]);

  return { payment, loading: state.loading, error: state.error };
}
