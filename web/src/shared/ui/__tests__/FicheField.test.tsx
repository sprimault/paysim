// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SectionTitle, Field } from '@/shared/ui/FicheField';

// Ces deux primitives sont partagées par les trois fiches de détail —
// paiement, moyen de paiement, abonnement. Elles étaient recopiées dans
// chacune ; ce qui change ici change les trois écrans à la fois.

describe('SectionTitle', () => {
  it('rend un titre de niveau 3', () => {
    render(<SectionTitle>Client</SectionTitle>);
    expect(screen.getByRole('heading', { level: 3, name: 'Client' })).toBeInTheDocument();
  });
});

describe('Field', () => {
  it('associe le libellé à sa valeur', () => {
    render(
      <dl>
        <Field label="Marque" value="VISA" />
      </dl>,
    );
    expect(screen.getByText('Marque')).toBeInTheDocument();
    expect(screen.getByText('VISA')).toBeInTheDocument();
  });

  it('accepte une valeur composée, pas seulement du texte', () => {
    render(
      <dl>
        <Field label="Jeton" value={<code>abc123</code>} />
      </dl>,
    );
    expect(screen.getByText('abc123').tagName).toBe('CODE');
  });

  // `wide` sert aux valeurs qui ne tiennent pas sur une demi-largeur —
  // un identifiant long, une charge utile.
  it('occupe les deux colonnes quand wide est posé', () => {
    const { container } = render(
      <dl>
        <Field label="UUID" value="0000-1111" wide />
      </dl>,
    );
    expect(container.querySelector('.sm\\:col-span-2')).not.toBeNull();
  });

  it('reste sur une colonne par défaut', () => {
    const { container } = render(
      <dl>
        <Field label="Montant" value="15,00 €" />
      </dl>,
    );
    expect(container.querySelector('.sm\\:col-span-2')).toBeNull();
  });
});
