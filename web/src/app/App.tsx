// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Routes, Route, Navigate } from 'react-router';
import { PaymentList } from '../features/payment-list/ui/PaymentList';
import { PaymentDetail } from '../features/payment-detail/ui/PaymentDetail';
import { WebhookDetail } from '../features/webhook-detail/ui/WebhookDetail';
import { Header } from '../widgets/header/Header';
import { ToastContainer } from '../shared/ui/Toast';

export function App() {
  return (
    <div className="min-h-screen bg-zinc-50 text-zinc-900 dark:bg-zinc-950 dark:text-zinc-100">
      <Header />
      <Routes>
        <Route path="/" element={<PaymentList />} />
        <Route path="/payments/:uuid" element={<PaymentDetail />} />
        <Route path="/webhooks/:id" element={<WebhookDetail />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      <ToastContainer />
    </div>
  );
}
