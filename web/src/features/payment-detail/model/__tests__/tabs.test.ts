// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { TAB_IDS, TAB_LABEL_KEYS, TAB_WITH_COUNTER, type TabId } from '@/features/payment-detail/model/tabs';

describe('tabs', () => {
  it('TAB_LABEL_KEYS a une entrée pour chaque TabId de TAB_IDS', () => {
    for (const id of TAB_IDS) {
      expect(TAB_LABEL_KEYS[id]).toBeDefined();
      expect(TAB_LABEL_KEYS[id].length).toBeGreaterThan(0);
    }
  });

  it('TAB_LABEL_KEYS n\'a pas de clé orpheline hors de TAB_IDS', () => {
    const ids = new Set<TabId>(TAB_IDS);
    for (const k of Object.keys(TAB_LABEL_KEYS) as TabId[]) {
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
