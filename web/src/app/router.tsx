// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { createBrowserRouter, Navigate } from 'react-router';
import { App } from '@/app/App';
import { PaymentDetail } from '@/features/payment-detail/ui/PaymentDetail';
import { PaymentList } from '@/features/payment-list/ui/PaymentList';
import { SubscriptionDetail } from '@/features/subscription-detail/ui/SubscriptionDetail';
import { SubscriptionList } from '@/features/subscription-list/ui/SubscriptionList';
import { WebhookDetail } from '@/features/webhook-detail/ui/WebhookDetail';
import { getBasePath } from '@/shared/api/basePath';

/**
 * Router principal — mode data (`createBrowserRouter`, react-router
 * v6.4+ / v7). Choix explicite plutôt que Routes/Route déclaratif :
 * prépare l'usage éventuel de loaders / actions co-localisés avec les
 * routes, aligné sur le pattern Cadensio, et rend l'ajout d'une route
 * mécanique (un objet dans children).
 *
 * Chacun sa responsabilité :
 *   - App.tsx : layout root (Header + Outlet + ToastContainer + SSE).
 *   - router.tsx : mapping URL → composant feature.
 *
 * `basename` propage le préfixe du base path Paysim quand l'ingress
 * sert l'app sous un sous-chemin — invariant web.md « aucun chemin
 * absolu en dur ». `undefined` en dev standalone (chemins relatifs à
 * la racine, proxy Vite gère la redirection).
 */
export const router = createBrowserRouter(
  [
    {
      path: '/',
      element: <App />,
      children: [
        { index: true, element: <PaymentList /> },
        { path: 'payments/:uuid', element: <PaymentDetail /> },
        { path: 'webhooks/:id', element: <WebhookDetail /> },
        { path: 'subscriptions', element: <SubscriptionList /> },
        { path: 'subscriptions/:id', element: <SubscriptionDetail /> },
        { path: '*', element: <Navigate to="/" replace /> },
      ],
    },
  ],
  { basename: getBasePath() || undefined },
);
