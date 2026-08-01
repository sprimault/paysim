// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';

/**
 * Store Zustand pour les toasts, isolé du composant qui les rend
 * (react-refresh n'aime pas mélanger export composant / non-composant).
 *
 * `toast` : API impérative utilisable depuis un callback ou un catch
 * hors composant. Elle contourne le hook et pousse directement dans
 * le store.
 */

export type ToastTone = 'success' | 'error' | 'info' | 'warning';

export interface Toast {
  id: number;
  tone: ToastTone;
  title: string;
  message?: string;
}

interface ToastState {
  toasts: Toast[];
  push: (t: Omit<Toast, 'id'>) => void;
  dismiss: (id: number) => void;
}

// Compteur monotone — évite les collisions d'id si deux toasts sont
// poussés dans la même ms. Pas besoin d'uuid pour de la volée UI.
let nextId = 1;

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  push: (t) => set((s) => ({ toasts: [...s.toasts, { ...t, id: nextId++ }] })),
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));

export const toast = {
  success: (title: string, message?: string) =>
    useToastStore.getState().push({ tone: 'success', title, message }),
  error: (title: string, message?: string) =>
    useToastStore.getState().push({ tone: 'error', title, message }),
  info: (title: string, message?: string) =>
    useToastStore.getState().push({ tone: 'info', title, message }),
  warning: (title: string, message?: string) =>
    useToastStore.getState().push({ tone: 'warning', title, message }),
};
