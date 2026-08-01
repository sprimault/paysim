// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, beforeEach } from 'vitest';
import { toast, useToastStore } from '../toastStore';

describe('toastStore', () => {
  beforeEach(() => {
    useToastStore.setState({ toasts: [] });
  });

  it('push ajoute un toast avec id auto-incrémenté', () => {
    useToastStore.getState().push({ tone: 'info', title: 'A' });
    useToastStore.getState().push({ tone: 'info', title: 'B' });
    const toasts = useToastStore.getState().toasts;
    expect(toasts).toHaveLength(2);
    expect(toasts[0].title).toBe('A');
    expect(toasts[1].title).toBe('B');
    expect(toasts[1].id).toBeGreaterThan(toasts[0].id);
  });

  it('dismiss retire le toast ciblé', () => {
    useToastStore.getState().push({ tone: 'info', title: 'A' });
    useToastStore.getState().push({ tone: 'info', title: 'B' });
    const [a] = useToastStore.getState().toasts;
    useToastStore.getState().dismiss(a.id);
    const remaining = useToastStore.getState().toasts;
    expect(remaining).toHaveLength(1);
    expect(remaining[0].title).toBe('B');
  });

  it('toast.success pousse un toast de tone success', () => {
    toast.success('ok', 'détail');
    const t = useToastStore.getState().toasts[0];
    expect(t.tone).toBe('success');
    expect(t.title).toBe('ok');
    expect(t.message).toBe('détail');
  });

  it.each(['success', 'error', 'info', 'warning'] as const)(
    'toast.%s pousse un tone correspondant',
    (kind) => {
      toast[kind]('titre');
      expect(useToastStore.getState().toasts[0].tone).toBe(kind);
    },
  );
});
