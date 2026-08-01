// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Définition des onglets de la vue Détail paiement. Isolée du composant
 * pour rester conforme FSD (ui/ pour le rendu, model/ pour les types
 * et données constantes). Le composant reconstitue les badges (JSX)
 * à partir de ces valeurs — le JSX n'a rien à faire dans model/.
 */

export type TabId = 'overview' | 'timeline' | 'webhooks' | 'payload';

// Ordre d'affichage des onglets — utilisé aussi pour vérifier
// l'exhaustivité côté tests.
export const TAB_IDS: readonly TabId[] = ['overview', 'timeline', 'webhooks', 'payload'];

export const TAB_LABELS: Record<TabId, string> = {
  overview: 'Aperçu',
  timeline: 'Journal',
  webhooks: 'Webhooks',
  payload: 'Charge utile',
};

// Onglets qui affichent un badge de compteur — le composant fournit la
// valeur (nombre d'événements, de webhooks), on lui indique juste ceux
// qui en méritent un.
export const TAB_WITH_COUNTER: readonly TabId[] = ['timeline', 'webhooks'];
