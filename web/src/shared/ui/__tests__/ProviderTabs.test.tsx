// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ProviderTabs } from '@/shared/ui/ProviderTabs';

const items = [
  { provider: 'payzen' },
  { provider: 'payzen' },
  { provider: 'systempay' },
];

function afficher(value = '') {
  const onChange = vi.fn();
  render(
    <ProviderTabs
      value={value}
      onChange={onChange}
      items={items}
      providerOf={(i) => i.provider}
    />,
  );
  return onChange;
}

const compte = (nom: RegExp) =>
  screen.getByRole('tab', { name: nom }).textContent?.replace(/\D/g, '');

describe('<ProviderTabs />', () => {
  // Tous les onglets doivent être chiffrés au premier rendu : c'est ce
  // qu'on cherche en balayant la barre, et ouvrir chaque marque pour
  // découvrir qu'elle est vide coûte autant de clics que de marques.
  it('chiffre tous les onglets sans qu’on en ouvre aucun', () => {
    afficher();
    expect(compte(/^Tous/)).toBe('3');
    expect(compte(/^PayZen/)).toBe('2');
    expect(compte(/^Systempay/)).toBe('1');
    // Une marque sans entrée annonce zéro plutôt que rien : l'absence
    // de pastille se lirait comme une absence d'information.
    expect(compte(/^Scellius/)).toBe('0');
    expect(compte(/^Lyra Collect/)).toBe('0');
  });

  // Le compte porte sur la collection entière, il ne suit donc pas
  // l'onglet actif — sans quoi ouvrir Systempay ferait tomber « Tous »
  // à 1.
  it('ne dépend pas de l’onglet sélectionné', () => {
    afficher('systempay');
    expect(compte(/^Tous/)).toBe('3');
    expect(compte(/^PayZen/)).toBe('2');
  });

  it('remonte la marque choisie', async () => {
    const user = userEvent.setup();
    const onChange = afficher();
    await user.click(screen.getByRole('tab', { name: /^Systempay/ }));
    expect(onChange).toHaveBeenCalledWith('systempay');
  });
});
