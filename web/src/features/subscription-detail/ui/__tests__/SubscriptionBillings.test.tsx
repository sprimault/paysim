// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SubscriptionBillings } from '@/features/subscription-detail/ui/SubscriptionBillings';
import type { PaymentSummary } from '@/shared/model';

const echeance: PaymentSummary = {
  uuid: 'uuid-1',
  provider: 'payzen',
  orderId: 'ECH-1',
  amount: 2990,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-01T12:00:00Z',
  updatedAt: '2026-08-01T12:00:00Z',
  webhookCount: 0,
  webhookReplayCount: 0,
};

/** Capture l'URL appelée et sert la réponse fournie. */
function mockFetch(payments: PaymentSummary[]) {
  const urls: string[] = [];
  vi.spyOn(global, 'fetch').mockImplementation(async (input) => {
    urls.push(String(input));
    return new Response(JSON.stringify(payments), {
      headers: { 'content-type': 'application/json' },
    });
  });
  return urls;
}

function renderBillings(subscriptionId = 'sub-A') {
  return render(
    <MemoryRouter>
      <SubscriptionBillings subscriptionId={subscriptionId} />
    </MemoryRouter>,
  );
}

describe('<SubscriptionBillings />', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Le filtre est côté serveur : sans le paramètre, l'écran afficherait
  // tous les paiements de l'instance comme s'ils venaient de cet
  // abonnement.
  it('interroge l\'API avec le filtre subscriptionId', async () => {
    const urls = mockFetch([echeance]);
    renderBillings('sub-A');
    await waitFor(() => {
      expect(screen.getByText('ECH-1')).toBeInTheDocument();
    });
    expect(urls.some((u) => u.includes('subscriptionId=sub-A'))).toBe(true);
  });

  it('rend une ligne par échéance, montant et état', async () => {
    mockFetch([echeance, { ...echeance, uuid: 'uuid-2', orderId: 'ECH-2' }]);
    renderBillings();
    await waitFor(() => {
      expect(screen.getByText('2 échéances')).toBeInTheDocument();
    });
    expect(screen.getByText('ECH-1')).toBeInTheDocument();
    expect(screen.getByText('ECH-2')).toBeInTheDocument();
    expect(screen.getAllByText('Payé')).toHaveLength(2);
  });

  it('chaque ligne mène au paiement', async () => {
    mockFetch([echeance]);
    renderBillings();
    await waitFor(() => {
      expect(screen.getByRole('link')).toHaveAttribute('href', '/payments/uuid-1');
    });
  });

  // Un prélèvement récurrent refusé en 51 se reconduit, un 43 impose de
  // réclamer une autre carte : c'est ici que le motif décide de la suite.
  it('affiche le code du motif de refus, libellé en infobulle', async () => {
    mockFetch([
      {
        ...echeance,
        state: 'declined',
        declineCode: '51',
        declineMessage: 'provision insuffisante',
      },
    ]);
    renderBillings();
    await waitFor(() => {
      expect(screen.getByText('51')).toBeInTheDocument();
    });
    // L'infobulle porte sur l'etat et son code d'un seul tenant. Elle ne
    // porte pas d'aria-label ici : la ligne entiere est un lien, et un
    // element focusable dans un <a> est du contenu interactif imbrique.
    const zone = screen.getByText('51').parentElement;
    if (!zone) throw new Error('zone survolable absente');
    expect(zone).toContainElement(screen.getByText('Refusé'));

    fireEvent.mouseEnter(zone);
    expect(screen.getByRole('tooltip')).toHaveTextContent('provision insuffisante');
  });

  it('affiche un état vide quand rien n\'a été prélevé', async () => {
    mockFetch([]);
    renderBillings();
    await waitFor(() => {
      expect(
        screen.getByText('Aucune échéance produite pour le moment.'),
      ).toBeInTheDocument();
    });
  });

  // Un échec de lecture ne doit pas masquer la fiche de l'abonnement.
  it('un échec de chargement laisse le bloc vide sans faire tomber l\'écran', async () => {
    vi.spyOn(global, 'fetch').mockRejectedValue(new Error('reseau'));
    renderBillings();
    await waitFor(() => {
      expect(
        screen.getByText('Aucune échéance produite pour le moment.'),
      ).toBeInTheDocument();
    });
  });
});
