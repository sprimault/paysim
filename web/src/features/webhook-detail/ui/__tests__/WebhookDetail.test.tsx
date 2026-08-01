// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { WebhookDetail } from '@/features/webhook-detail/ui/WebhookDetail';
import { useWebhookStore } from '@/entities/webhook/model/webhookStore';
import type { WebhookDetail as WebhookDetailDTO } from '@/shared/model';

const originalFetch = globalThis.fetch;

function renderAt(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/webhooks/${id}`]}>
      <Routes>
        <Route path="/webhooks/:id" element={<WebhookDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

const detail: WebhookDetailDTO = {
  id: 'wh-1',
  url: 'https://marchand.example/callback',
  status: 'delivered',
  statusCode: 200,
  attempts: 1,
  createdAt: '2026-08-01T10:00:00Z',
  completedAt: '2026-08-01T10:00:01Z',
  headers: { 'Content-Type': 'application/json' },
  body: 'kr-hash=abc',
};

describe('WebhookDetail', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    useWebhookStore.getState().clear();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('affiche le webhook chargé depuis le store', () => {
    useWebhookStore.getState().setDetail(detail);
    renderAt('wh-1');
    expect(screen.getByText('Livré')).toBeInTheDocument();
    expect(screen.getByText('HTTP 200')).toBeInTheDocument();
    expect(screen.getByText('wh-1')).toBeInTheDocument();
    expect(screen.getByText('kr-hash=abc')).toBeInTheDocument();
  });

  it('affiche une erreur si l\'API échoue', async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('not found', { status: 404 }),
    );
    renderAt('inexistant');
    expect(await screen.findByText(/erreur/i)).toBeInTheDocument();
  });

  it('rend le bouton Rejouer', () => {
    useWebhookStore.getState().setDetail(detail);
    renderAt('wh-1');
    expect(screen.getByRole('button', { name: /rejouer/i })).toBeInTheDocument();
  });
});
