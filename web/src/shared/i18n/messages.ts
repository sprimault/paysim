// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Dictionnaire des messages FR/EN pour l'UI Paysim.
 *
 * Format : `feature.item.key`, deux niveaux max, en snake-case si
 * nécessaire dans le segment terminal (rare). Ajouter une entrée
 * dans les DEUX locales — le type Lang garantit qu'un oubli fait
 * échouer le build.
 *
 * Commit initial : Header + tabs (haut de page). Les autres vues
 * seront traduites incrémentalement, une par commit.
 */

export const messages = {
  fr: {
    'header.nav.payments': 'Paiements',
    'header.nav.subscriptions': 'Abonnements',
    'header.nav.paymentMethods': 'Moyens de paiement',
    'header.nav.aria': 'Navigation principale',
    'header.connection.connected': 'Connecté',
    'header.connection.disconnected': 'Déconnecté',
    'header.connection.titleConnected': 'Flux SSE ouvert',
    'header.connection.titleDisconnected': 'Flux SSE fermé',
    'header.lang.label': 'Langue',
    'header.lang.french': 'Français',
    'header.lang.english': 'Anglais',
    'header.theme.label': 'Thème',
    'header.theme.light': 'Clair',
    'header.theme.dark': 'Sombre',
    'header.theme.system': 'Système',
    'providerTabs.all': 'Tous',
  },
  en: {
    'header.nav.payments': 'Payments',
    'header.nav.subscriptions': 'Subscriptions',
    'header.nav.paymentMethods': 'Payment methods',
    'header.nav.aria': 'Main navigation',
    'header.connection.connected': 'Connected',
    'header.connection.disconnected': 'Disconnected',
    'header.connection.titleConnected': 'SSE stream open',
    'header.connection.titleDisconnected': 'SSE stream closed',
    'header.lang.label': 'Language',
    'header.lang.french': 'French',
    'header.lang.english': 'English',
    'header.theme.label': 'Theme',
    'header.theme.light': 'Light',
    'header.theme.dark': 'Dark',
    'header.theme.system': 'System',
    'providerTabs.all': 'All',
  },
} as const;

export type Lang = keyof typeof messages;

// Les deux locales doivent avoir exactement les mêmes clés — sinon
// erreur de compilation. Cette contrainte est ce qui garantit qu'un
// ajout de string ne peut pas oublier une traduction.
export type MessageKey = keyof (typeof messages)['fr'] & keyof (typeof messages)['en'];
