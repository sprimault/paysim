// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { copyToClipboard } from '@/shared/lib/clipboard';

/** Remplace navigator.clipboard, absent de jsdom comme d'un site en http. */
function poserClipboard(writeText: unknown) {
  Object.defineProperty(navigator, 'clipboard', {
    value: writeText === undefined ? undefined : { writeText },
    configurable: true,
    writable: true,
  });
}

describe('copyToClipboard', () => {
  beforeEach(() => {
    // jsdom n'implémente pas execCommand : sans ce bouchon, le repli
    // lèverait au lieu de rendre un booléen.
    document.execCommand = vi.fn(() => true);
  });

  afterEach(() => {
    poserClipboard(undefined);
    vi.restoreAllMocks();
  });

  it('passe par l\'API presse-papier quand elle existe', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    poserClipboard(writeText);
    expect(await copyToClipboard('curl ...')).toBe(true);
    expect(writeText).toHaveBeenCalledWith('curl ...');
    expect(document.execCommand).not.toHaveBeenCalled();
  });

  // navigator.clipboard n'existe qu'en https ou sur localhost. Paysim se
  // sert couramment en clair sur une IP de reseau local, ou l'objet est
  // tout simplement absent.
  it('se rabat sur la selection quand l\'API est absente', async () => {
    poserClipboard(undefined);
    expect(await copyToClipboard('curl ...')).toBe(true);
    expect(document.execCommand).toHaveBeenCalledWith('copy');
  });

  it('se rabat aussi quand l\'API rejette', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('NotAllowedError'));
    poserClipboard(writeText);
    expect(await copyToClipboard('curl ...')).toBe(true);
    expect(document.execCommand).toHaveBeenCalledWith('copy');
  });

  // Le silence est ce qui rendait le defaut invisible : l'echec doit
  // remonter pour que l'appelant puisse le dire.
  it('rend false quand les deux voies echouent', async () => {
    poserClipboard(undefined);
    document.execCommand = vi.fn(() => false);
    expect(await copyToClipboard('curl ...')).toBe(false);
  });

  // Un champ laisse derriere lui deplacerait la mise en page a chaque
  // copie, et s'accumulerait a chaque clic.
  it('ne laisse aucun champ dans le document', async () => {
    poserClipboard(undefined);
    await copyToClipboard('curl ...');
    expect(document.querySelectorAll('textarea')).toHaveLength(0);
  });

  it('nettoie meme quand la copie leve', async () => {
    poserClipboard(undefined);
    document.execCommand = vi.fn(() => {
      throw new Error('refus');
    });
    expect(await copyToClipboard('curl ...')).toBe(false);
    expect(document.querySelectorAll('textarea')).toHaveLength(0);
  });
});
