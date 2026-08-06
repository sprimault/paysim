// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo } from 'react';
import { fetchPayments } from '@/entities/payment/api/paymentApi';
import { usePaymentStore } from '@/entities/payment/model/paymentStore';
import { fetchPaymentMethods } from '@/entities/payment-method/api/paymentMethodApi';
import { usePaymentMethodStore } from '@/entities/payment-method/model/paymentMethodStore';
import { fetchSubscriptions } from '@/entities/subscription/api/subscriptionApi';
import { useSubscriptionStore } from '@/entities/subscription/model/subscriptionStore';

/** Décomptes affichés en pastille sur les onglets de navigation. */
export interface NavCounts {
  payments: number;
  subscriptions: number;
  paymentMethods: number;
}

/**
 * Alimente les pastilles de la navigation.
 *
 * Les stores d'entités ne se remplissent qu'à la visite de leur page :
 * un compteur qui ne renseignerait que sur l'onglet déjà ouvert
 * n'aurait aucun intérêt, puisque son rôle est justement de signaler
 * ce qu'on n'a pas encore regardé. D'où l'amorçage au montage — une
 * requête par collection non chargée, une seule fois pour la session.
 *
 * Ensuite, les paiements suivent le flux SSE (`usePaysimEvents` pousse
 * dans le même store) et se mettent donc à jour en direct. Abonnements
 * et moyens de paiement n'ont pas d'événement serveur à ce jour : leur
 * pastille se rafraîchit à la visite de la page correspondante, dont
 * le hook de liste refetch au montage. C'est une limite assumée, pas
 * un oubli — la corriger demanderait d'émettre ces événements côté Go.
 */
export function useNavCounts(): NavCounts {
  const payments = usePaymentStore((s) => s.payments);
  const subscriptions = useSubscriptionStore((s) => s.subscriptions);
  const methods = usePaymentMethodStore((s) => s.methods);

  useEffect(() => {
    // On lit l'état hors du rendu : ce qui compte est ce qui a déjà été
    // chargé au moment de l'amorçage, pas ce qu'un autre composant est
    // en train de charger dans le même cycle.
    const stores = [
      {
        charge: usePaymentStore.getState().listLoaded,
        run: () => fetchPayments().then(usePaymentStore.getState().setList),
      },
      {
        charge: useSubscriptionStore.getState().listLoaded,
        run: () => fetchSubscriptions().then(useSubscriptionStore.getState().setList),
      },
      {
        charge: usePaymentMethodStore.getState().listLoaded,
        run: () => fetchPaymentMethods().then(usePaymentMethodStore.getState().setList),
      },
    ];

    for (const { charge, run } of stores) {
      // Un compteur muet vaut mieux qu'un bandeau d'erreur : l'échec
      // se verra de toute façon sur la page de la collection.
      if (!charge) void run().catch(() => undefined);
    }
  }, []);

  return useMemo(
    () => ({
      payments: Object.keys(payments).length,
      subscriptions: Object.keys(subscriptions).length,
      paymentMethods: Object.keys(methods).length,
    }),
    [payments, subscriptions, methods],
  );
}
