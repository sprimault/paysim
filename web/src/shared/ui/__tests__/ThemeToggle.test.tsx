// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';
import { ThemeToggle } from '@/shared/ui/ThemeToggle';
import { STORAGE_KEY } from '@/shared/lib/theme';

describe('<ThemeToggle />', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove('dark');
  });

  it('affiche trois options (clair, système, sombre)', () => {
    render(<ThemeToggle />);
    expect(screen.getByRole('radio', { name: 'Clair' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Système' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Sombre' })).toBeInTheDocument();
  });

  it('marque système comme actif par défaut', () => {
    render(<ThemeToggle />);
    expect(screen.getByRole('radio', { name: 'Système' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByRole('radio', { name: 'Clair' })).toHaveAttribute(
      'aria-checked',
      'false',
    );
  });

  it('bascule vers dark et applique la classe sur <html>', async () => {
    const user = userEvent.setup();
    render(<ThemeToggle />);
    await user.click(screen.getByRole('radio', { name: 'Sombre' }));
    expect(screen.getByRole('radio', { name: 'Sombre' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(localStorage.getItem(STORAGE_KEY)).toBe('dark');
  });

  it('bascule vers light et retire la classe dark', async () => {
    const user = userEvent.setup();
    document.documentElement.classList.add('dark');
    render(<ThemeToggle />);
    await user.click(screen.getByRole('radio', { name: 'Clair' }));
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    expect(localStorage.getItem(STORAGE_KEY)).toBe('light');
  });
});
