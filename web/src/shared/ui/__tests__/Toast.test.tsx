// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { ToastContainer } from '@/shared/ui/Toast';
import { toast, useToastStore } from '@/shared/ui/toastStore';

describe('ToastContainer', () => {
  beforeEach(() => {
    useToastStore.setState({ toasts: [] });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('affiche un toast poussé via l\'API', () => {
    render(<ToastContainer />);
    act(() => toast.success('Sauvegardé'));
    expect(screen.getByText('Sauvegardé')).toBeInTheDocument();
  });

  it('affiche titre et message', () => {
    render(<ToastContainer />);
    act(() => toast.error('Erreur', 'Détail'));
    expect(screen.getByText('Erreur')).toBeInTheDocument();
    expect(screen.getByText('Détail')).toBeInTheDocument();
  });

  it('retire automatiquement le toast après 5s', () => {
    render(<ToastContainer />);
    act(() => toast.info('Éphémère'));
    expect(screen.getByText('Éphémère')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.queryByText('Éphémère')).not.toBeInTheDocument();
  });

  it('se ferme au clic sur le bouton Fermer', () => {
    render(<ToastContainer />);
    act(() => toast.info('À fermer'));
    // fireEvent est synchrone (userEvent avec fake timers pose des soucis
    // de timing — le clic n'a besoin d'aucune animation ici).
    fireEvent.click(screen.getByRole('button', { name: 'Fermer' }));
    expect(screen.queryByText('À fermer')).not.toBeInTheDocument();
  });
});
