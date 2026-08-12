// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { usePaymentColumns } from '@/features/payment-list/ui/paymentColumns';
import { DataTable } from '@/shared/ui/DataTable';
import type { PaymentSummary } from '@/shared/model';

const p: PaymentSummary = {
  uuid: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
  provider: 'payzen',
  orderId: 'CMD-42',
  amount: 1299,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-01T12:00:00Z',
  updatedAt: '2026-08-01T12:05:00Z',
  webhookCount: 3,
};

/**
 * Les colonnes ne se rendent que montées dans la table : c'est un hook,
 * et le vrai contrat est ce que l'utilisateur lit à l'écran.
 */
function Table({ rows }: { rows: PaymentSummary[] }) {
  const columns = usePaymentColumns({ showProvider: true });
  return <DataTable columns={columns} rows={rows} rowKey={(r) => r.uuid} />;
}

function renderRow(payment: PaymentSummary = p) {
  return render(
    <MemoryRouter>
      <Table rows={[payment]} />
    </MemoryRouter>,
  );
}

describe('colonnes de la liste des paiements', () => {
  it('rend le libellé d\'état, le montant, la devise et l\'orderId', () => {
    renderRow();
    expect(screen.getByText('Payé')).toBeInTheDocument();
    expect(screen.getByText('12,99')).toBeInTheDocument();
    expect(screen.getByText('EUR')).toBeInTheDocument();
    expect(screen.getByText('CMD-42')).toBeInTheDocument();
  });

  it('link vers la page détail avec le bon uuid', () => {
    renderRow();
    const link = screen.getByRole('link', { name: /ouvrir/i });
    expect(link).toHaveAttribute('href', `/payments/${p.uuid}`);
  });

  // Le motif decide de la suite chez le marchand : un 51 se retente, un
  // 43 impose de reclamer une autre carte. Il etait livre par l'API et
  // affiche nulle part — il fallait ouvrir la charge utile et lire du
  // JSON pour le trouver.
  // L'infobulle porte sur la cellule entiere, etat compris : viser le
  // badge de deux caracteres demandait une precision que personne ne
  // fournit pour lire un libelle.
  it('affiche le code du motif de refus, libelle en infobulle sur toute la cellule', () => {
    renderRow({ ...p, state: 'declined', declineCode: '51', declineMessage: 'provision insuffisante' });
    const badge = screen.getByText('51');
    expect(badge).toBeInTheDocument();

    // La zone survolable couvre l'etat et son code d'un seul tenant.
    const zone = screen.getByLabelText('provision insuffisante');
    expect(zone).toContainElement(badge);
    expect(zone).toContainElement(screen.getByText('Refusé'));

    // Rien tant qu'on ne survole pas.
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    fireEvent.mouseEnter(zone);
    expect(screen.getByRole('tooltip')).toHaveTextContent('provision insuffisante');
    fireEvent.mouseLeave(zone);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  // Sans motif, l'etat ne doit rien annoncer a lire. On survole la
  // cellule elle-meme : chercher un curseur dans toute la ligne
  // attraperait celui du bouton de copie, qui a le sien a bon droit.
  it('sans motif, l\'etat n\'ouvre aucune infobulle', () => {
    renderRow({ ...p, state: 'declined' });
    const etat = screen.getByText('Refusé');
    fireEvent.mouseEnter(etat);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    // Et l'etat n'est pas devenu une zone survolable.
    expect(etat.closest('.cursor-pointer')).toBeNull();
  });

  // Un abandon ou une expiration n'ont pas de code bancaire : un badge
  // vide vaudrait moins que pas de badge du tout.
  it('sans motif, aucun badge de refus', () => {
    renderRow({ ...p, state: 'declined' });
    expect(screen.queryByTitle(/provision/)).not.toBeInTheDocument();
  });

  // Le nombre de livraisons repond a la question qui revient quand un
  // marchand dit n'avoir rien recu, sans avoir a ouvrir chaque fiche.
  it('affiche le nombre de livraisons', () => {
    renderRow();
    const ligne = screen.getAllByRole('row')[1];
    expect(within(ligne).getByText('3')).toBeInTheDocument();
  });

  // Le zero est grise plutot que masque : c'est precisement le paiement
  // qu'on cherche, pas une case a ignorer.
  it('grise un paiement sans livraison plutot que de le laisser vide', () => {
    renderRow({ ...p, webhookCount: 0 });
    const zero = within(screen.getAllByRole('row')[1]).getByText('0');
    expect(zero.className).toContain('text-zinc-400');
  });

  it('rend un bouton de copie pour l\'uuid', () => {
    renderRow();
    const copyButtons = screen.getAllByRole('button', { name: /copier/i });
    expect(copyButtons.length).toBeGreaterThan(0);
  });
});
