// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CopyCurlButton } from '@/entities/payment/ui/CopyCurlButton';
import { toast } from '@/shared/ui/toastStore';
import type { PaymentDetail } from '@/shared/model';

const { fetchPayment, copyToClipboard } = vi.hoisted(() => ({
  fetchPayment: vi.fn(),
  copyToClipboard: vi.fn(),
}));

vi.mock('@/entities/payment/api/paymentApi', () => ({ fetchPayment }));
vi.mock('@/shared/lib/clipboard', () => ({ copyToClipboard }));

const detail: PaymentDetail = {
  uuid: 'u-1',
  provider: 'payzen',
  orderId: 'CMD-1042',
  amount: 4990,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-11T10:00:00Z',
  updatedAt: '2026-08-11T10:00:01Z',
  events: [],
  customer: { email: 'bob@example.com' },
  metadata: { plan: 'pro' },
};

describe('<CopyCurlButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fetchPayment.mockResolvedValue(detail);
    copyToClipboard.mockResolvedValue(true);
  });

  const bouton = () =>
    screen.getByRole('button', { name: 'Copier la commande curl qui rejoue ce paiement' });

  // Le sommaire de la liste ne porte ni customer ni metadata : bâtir la
  // commande à partir de lui donnerait, sous la même icône, un rejeu
  // plus pauvre selon l'endroit d'où on l'a copié.
  it('copie une commande portant le contexte marchand', async () => {
    render(<CopyCurlButton uuid="u-1" />);
    fireEvent.click(bouton());
    await waitFor(() => expect(fetchPayment).toHaveBeenCalledWith('u-1'));
    const commande = copyToClipboard.mock.calls[0][0] as string;
    expect(commande).toContain('curl -X POST');
    expect(commande).toContain('bob@example.com');
    expect(commande).toContain('"plan":"pro"');
  });

  it('confirme la copie', async () => {
    render(<CopyCurlButton uuid="u-1" />);
    fireEvent.click(bouton());
    expect(await screen.findByRole('button', { name: 'Copié' })).toBeInTheDocument();
  });

  // Un échec muet est pire que pas de bouton : on croit avoir copié, et
  // on colle autre chose.
  it('signale une copie impossible', async () => {
    copyToClipboard.mockResolvedValue(false);
    const erreur = vi.spyOn(toast, 'error');
    render(<CopyCurlButton uuid="u-1" />);
    fireEvent.click(bouton());
    await waitFor(() =>
      expect(erreur).toHaveBeenCalledWith('Copie impossible depuis ce navigateur'),
    );
    expect(screen.queryByRole('button', { name: 'Copié' })).not.toBeInTheDocument();
  });

  it('signale un détail introuvable', async () => {
    fetchPayment.mockRejectedValue(new Error('404 not found'));
    const erreur = vi.spyOn(toast, 'error');
    render(<CopyCurlButton uuid="u-1" />);
    fireEvent.click(bouton());
    await waitFor(() =>
      expect(erreur).toHaveBeenCalledWith('Commande curl indisponible', '404 not found'),
    );
  });
});
