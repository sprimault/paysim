// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Routes, Route } from 'react-router';

/**
 * Squelette minimal de l'App — les vraies vues (Dashboard, PaymentDetail,
 * WebhookDetail) arrivent au sous-vertical 3b. Ce squelette vérifie
 * que le build fonctionne bout-en-bout : Vite → React → Router → Tailwind.
 */
export function App() {
  return (
    <div className="min-h-screen">
      <Routes>
        <Route path="/" element={<Placeholder title="Dashboard" hint="liste des paiements — vertical 3b" />} />
        <Route
          path="/payments/:uuid"
          element={<Placeholder title="Détail paiement" hint="journal + webhooks — vertical 3b" />}
        />
        <Route
          path="/webhooks/:id"
          element={<Placeholder title="Détail webhook" hint="requête / réponse — vertical 3b" />}
        />
      </Routes>
    </div>
  );
}

function Placeholder({ title, hint }: { title: string; hint: string }) {
  return (
    <main className="mx-auto flex min-h-screen max-w-4xl flex-col items-start gap-4 px-6 py-16">
      <span className="rounded-full bg-brand-100 px-3 py-1 text-xs font-medium uppercase tracking-wider text-brand-700">
        Paysim
      </span>
      <h1 className="text-4xl font-semibold tracking-tight text-zinc-900">{title}</h1>
      <p className="text-zinc-600">{hint}</p>
      <code className="mt-4 rounded bg-zinc-100 px-2 py-1 font-mono text-sm text-zinc-800">
        Vite + React + Router + Tailwind — ok
      </code>
    </main>
  );
}
