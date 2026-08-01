// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { TAB_IDS, TAB_LABELS, TAB_WITH_COUNTER, type TabId } from '../tabs';

describe('tabs', () => {
  it('TAB_LABELS a une entrée pour chaque TabId de TAB_IDS', () => {
    for (const id of TAB_IDS) {
      expect(TAB_LABELS[id]).toBeDefined();
      expect(TAB_LABELS[id].length).toBeGreaterThan(0);
    }
  });

  it('TAB_LABELS n\'a pas de clé orpheline hors de TAB_IDS', () => {
    const ids = new Set<TabId>(TAB_IDS);
    for (const k of Object.keys(TAB_LABELS) as TabId[]) {
      expect(ids.has(k)).toBe(true);
    }
  });

  it('TAB_WITH_COUNTER ne référence que des ids déclarés', () => {
    const ids = new Set<TabId>(TAB_IDS);
    for (const id of TAB_WITH_COUNTER) {
      expect(ids.has(id)).toBe(true);
    }
  });
});
