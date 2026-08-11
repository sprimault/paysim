// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ReplayLastWebhookButton } from '@/features/payment-list/ui/ReplayLastWebhookButton';
import { toast } from '@/shared/ui/toastStore';
import type { PaymentSummary } from '@/shared/model';
import type { WebhookEntry } from '@/shared/model';

const { fetchWebhooksOfPayment, replayWebhook } = vi.hoisted(() => ({
  fetchWebhooksOfPayment: vi.fn(),
  replayWebhook: vi.fn(),
}));

vi.mock('@/entities/webhook/api/webhookApi', () => ({
  fetchWebhooksOfPayment,
  replayWebhook,
}));

const paiement: PaymentSummary = {
  uuid: 'u-1',
  provider: 'payzen',
  orderId: 'CMD-1042',
  amount: 4990,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-11T10:00:00Z',
  updatedAt: '2026-08-11T10:00:01Z',
};

/** Livraison minimale — seuls l'id et la date pèsent sur le choix. */
function livraison(id: string, createdAt: string): WebhookEntry {
  return {
    id,
    url: 'http://sink/hook',
    status: 'delivered',
    attempts: 1,
    createdAt,
    completedAt: createdAt,
  };
}

describe('<ReplayLastWebhookButton />', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replayWebhook.mockResolvedValue({ newDeliveryId: 'wh-neuf' });
  });

  const bouton = () => screen.getByRole('button', { name: 'Rejouer la dernière livraison' });

  // C'est la dernière livraison qu'on rejoue, et l'API ne garantit pas
  // l'ordre : le tri est ici, pas dans l'espoir qu'il vienne d'ailleurs.
  it('rejoue la livraison la plus récente', async () => {
    fetchWebhooksOfPayment.mockResolvedValue([
      livraison('wh-vieux', '2026-08-11T10:00:00Z'),
      livraison('wh-recent', '2026-08-11T11:00:00Z'),
      livraison('wh-milieu', '2026-08-11T10:30:00Z'),
    ]);
    const succes = vi.spyOn(toast, 'success');
    render(<ReplayLastWebhookButton payment={paiement} />);
    fireEvent.click(bouton());
    await waitFor(() => expect(replayWebhook).toHaveBeenCalledWith('wh-recent'));
    expect(succes).toHaveBeenCalledWith('Webhook rejoué', 'wh-neuf');
  });

  // Le filtre est demandé au serveur : la fenêtre locale ne garde que
  // les dernières livraisons, un paiement ancien s'y verrait déclaré
  // sans livraison alors que la base en a.
  it('demande les livraisons de ce paiement au serveur', async () => {
    fetchWebhooksOfPayment.mockResolvedValue([livraison('wh-1', '2026-08-11T10:00:00Z')]);
    render(<ReplayLastWebhookButton payment={paiement} />);
    fireEvent.click(bouton());
    await waitFor(() => expect(fetchWebhooksOfPayment).toHaveBeenCalledWith('u-1'));
  });

  // « Rien n'est jamais parti » est une réponse, pas une panne — souvent
  // même ce qu'on cherchait à savoir.
  it('annonce l\'absence de livraison sans crier à l\'erreur', async () => {
    fetchWebhooksOfPayment.mockResolvedValue([]);
    const info = vi.spyOn(toast, 'info');
    const erreur = vi.spyOn(toast, 'error');
    render(<ReplayLastWebhookButton payment={paiement} />);
    fireEvent.click(bouton());
    await waitFor(() => expect(info).toHaveBeenCalledWith('Aucune livraison à rejouer', 'CMD-1042'));
    expect(replayWebhook).not.toHaveBeenCalled();
    expect(erreur).not.toHaveBeenCalled();
  });

  it('remonte l\'échec du rejeu', async () => {
    fetchWebhooksOfPayment.mockResolvedValue([livraison('wh-1', '2026-08-11T10:00:00Z')]);
    replayWebhook.mockRejectedValue(new Error('502 Bad Gateway'));
    const erreur = vi.spyOn(toast, 'error');
    render(<ReplayLastWebhookButton payment={paiement} />);
    fireEvent.click(bouton());
    await waitFor(() =>
      expect(erreur).toHaveBeenCalledWith('Rejeu échoué', '502 Bad Gateway'),
    );
  });

  // Deux clics pendant l'aller-retour produiraient deux livraisons là
  // où l'utilisateur en a demandé une.
  it('ne rejoue pas deux fois sur un double-clic', async () => {
    let debloquer: (v: WebhookEntry[]) => void = () => undefined;
    fetchWebhooksOfPayment.mockReturnValue(
      new Promise<WebhookEntry[]>((resolve) => {
        debloquer = resolve;
      }),
    );
    render(<ReplayLastWebhookButton payment={paiement} />);
    fireEvent.click(bouton());
    fireEvent.click(bouton());
    debloquer([livraison('wh-1', '2026-08-11T10:00:00Z')]);
    await waitFor(() => expect(replayWebhook).toHaveBeenCalledTimes(1));
    expect(fetchWebhooksOfPayment).toHaveBeenCalledTimes(1);
  });
});
