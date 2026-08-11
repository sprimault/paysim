// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { buildReplayCurl } from '@/entities/payment/lib/curlCommand';
import type { PaymentInStore } from '@/entities/payment/model/paymentStore';

const base: PaymentInStore = {
  uuid: 'u-1',
  provider: 'payzen',
  orderId: 'CMD-1042',
  amount: 4990,
  currency: 'EUR',
  state: 'captured',
  createdAt: '2026-08-11T10:00:00Z',
  updatedAt: '2026-08-11T10:00:01Z',
};

/** Le corps JSON, extrait de la commande, pour l'inspecter champ à champ. */
function corps(commande: string): Record<string, unknown> {
  const m = commande.match(/-d '(.*)'$/);
  if (!m) throw new Error(`pas de corps dans : ${commande}`);
  return JSON.parse(m[1].replace(/'\\''/g, "'"));
}

describe('buildReplayCurl', () => {
  it('vise l\'API générique de création, en POST JSON', () => {
    const c = buildReplayCurl(base, 'http://paysim.test');
    expect(c).toContain("curl -X POST 'http://paysim.test/paysim/api/v1/payments'");
    expect(c).toContain("-H 'Content-Type: application/json'");
  });

  it('reprend provider, montant, devise et commande', () => {
    expect(corps(buildReplayCurl(base, 'http://x'))).toEqual({
      provider: 'payzen',
      amount: 4990,
      currency: 'EUR',
      orderId: 'CMD-1042',
    });
  });

  // Le montant est en centimes entiers de bout en bout : le recopier
  // formaté produirait un rejeu à 49 centimes.
  it('garde le montant en centimes', () => {
    expect(corps(buildReplayCurl(base, 'http://x')).amount).toBe(4990);
  });

  it('reprend le contexte marchand quand il existe', () => {
    const avec: PaymentInStore = {
      ...base,
      customer: { email: 'bob@example.com', reference: 'client-1042' },
      metadata: { plan: 'pro' },
    };
    const c = corps(buildReplayCurl(avec, 'http://x'));
    expect(c.customer).toEqual({ email: 'bob@example.com', reference: 'client-1042' });
    expect(c.metadata).toEqual({ plan: 'pro' });
  });

  // Un objet vide n'apporte rien et allonge une commande déjà longue.
  it('omet des métadonnées vides', () => {
    expect(corps(buildReplayCurl({ ...base, metadata: {} }, 'http://x'))).not.toHaveProperty(
      'metadata',
    );
  });

  // Le rejeu one-click est le seul rejeu fidèle d'un paiement par carte.
  it('reprend l\'alias quand le paiement en porte un', () => {
    const c = corps(buildReplayCurl({ ...base, paymentMethodToken: 'tok-9' }, 'http://x'));
    expect(c.paymentMethodToken).toBe('tok-9');
  });

  // Paysim ne restitue que le PAN masqué : une carte reconstruite
  // donnerait une commande qui marche sans rejouer le même cas.
  it('ne fabrique jamais de carte', () => {
    expect(buildReplayCurl({ ...base, paymentMethodToken: 'tok-9' }, 'http://x')).not.toContain(
      'card',
    );
  });

  // Sans échappement, la commande se couperait en deux sans erreur —
  // le shell l'accepterait quand même.
  it('échappe les guillemets simples du corps', () => {
    const c = buildReplayCurl({ ...base, orderId: "L'ete" }, 'http://x');
    expect(c).toContain(`'\\''`);
    expect(corps(c).orderId).toBe("L'ete");
  });

  it('respecte le préfixe de service', () => {
    window.__PAYSIM_BASE_PATH__ = '/paysim';
    try {
      expect(buildReplayCurl(base, 'http://x')).toContain(
        "'http://x/paysim/paysim/api/v1/payments'",
      );
    } finally {
      delete window.__PAYSIM_BASE_PATH__;
    }
  });
});
