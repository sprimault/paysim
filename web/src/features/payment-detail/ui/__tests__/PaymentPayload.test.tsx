// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PaymentPayload } from '../PaymentPayload';
import type { WebhookDetail } from '../../../../shared/model';

const webhook: WebhookDetail = {
  id: 'wh-1',
  url: 'https://x.example/callback',
  status: 'delivered',
  statusCode: 200,
  attempts: 1,
  createdAt: '2026-08-01T10:00:00Z',
  completedAt: '2026-08-01T10:00:01Z',
  headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  body: 'kr-hash=abc123&kr-answer-type=V4%2FPayment&kr-answer=%7B%22orderStatus%22%3A%22PAID%22%7D',
};

describe('PaymentPayload', () => {
  it('rend le EmptyState quand aucun webhook', () => {
    render(<PaymentPayload />);
    expect(screen.getByText(/pas de charge utile/i)).toBeInTheDocument();
  });

  it('rend la section kr-answer et les autres champs', () => {
    const { container } = render(<PaymentPayload webhook={webhook} />);
    expect(screen.getByText('kr-answer')).toBeInTheDocument();
    expect(screen.getByText('Autres champs')).toBeInTheDocument();
    expect(container.textContent).toContain('orderStatus');
    expect(container.textContent).toContain('PAID');
    expect(container.textContent).toContain('kr-hash');
    expect(container.textContent).toContain('abc123');
  });
});
