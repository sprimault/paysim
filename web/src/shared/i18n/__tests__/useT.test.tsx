// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useT } from '@/shared/i18n/useT';
import { useLangStore } from '@/shared/i18n/store';

describe('useT', () => {
  // Un `t` qui change d'identité à chaque rendu relance tout effet qui
  // le déclare en dépendance. Quand cet effet écrit dans un store, il se
  // réveille lui-même : ClockControl relançait sa lecture de l'horloge
  // en boucle, une requête par rendu. La stabilité n'est donc pas une
  // optimisation, c'est ce qui empêche la boucle.
  it('garde la même identité entre deux rendus à langue constante', () => {
    const { result, rerender } = renderHook(() => useT());
    const premier = result.current;

    rerender();
    rerender();

    expect(result.current).toBe(premier);
  });

  it('rend une nouvelle fonction quand la langue change', () => {
    useLangStore.setState({ lang: 'fr' });
    const { result } = renderHook(() => useT());
    const enFrancais = result.current;

    act(() => {
      useLangStore.setState({ lang: 'en' });
    });

    expect(result.current).not.toBe(enFrancais);
  });

  it('traduit dans la langue courante', () => {
    useLangStore.setState({ lang: 'fr' });
    const { result } = renderHook(() => useT());
    const enFrancais = result.current('header.clock.title');

    act(() => {
      useLangStore.setState({ lang: 'en' });
    });

    expect(result.current('header.clock.title')).not.toBe('');
    expect(enFrancais).not.toBe('');
  });
});
