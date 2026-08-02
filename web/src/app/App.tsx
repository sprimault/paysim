// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Outlet } from 'react-router';
import { Header } from '@/widgets/header/Header';
import { ToastContainer } from '@/shared/ui/Toast';
import { usePaysimEvents } from '@/shared/hooks/usePaysimEvents';

/**
 * Layout root de l'application. Enveloppe toutes les routes via
 * `<Outlet />` — react-router data mode. Une seule connexion SSE
 * partagée pour toute l'app (source unique du statut « connecté »
 * du Header).
 *
 * Chacun sa responsabilité : ce fichier expose le layout et branche
 * les providers ; le mapping URL → composant vit dans router.tsx.
 * Cohérent avec le pattern Cadensio.
 */
export function App() {
  const { connected } = usePaysimEvents();
  return (
    <div className="min-h-screen bg-zinc-50 text-zinc-900 dark:bg-zinc-950 dark:text-zinc-100">
      <Header connected={connected} />
      <Outlet />
      <ToastContainer />
    </div>
  );
}
