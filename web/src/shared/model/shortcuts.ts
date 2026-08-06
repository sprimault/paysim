// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import type { Shortcut } from '@/shared/hooks/useKeyboardShortcuts';
import type { MessageKey } from '@/shared/i18n/messages';

/**
 * Table des raccourcis clavier de l'application.
 *
 * Source unique : le câblage et l'écran d'aide lisent les mêmes
 * entrées. Documenter les raccourcis à part de leur implémentation
 * garantirait qu'ils divergent — l'aide affiche alors des touches qui
 * ne font plus rien, ce qui est pire que pas d'aide du tout.
 *
 * Les raccourcis ne sont pas tous branchés au même endroit : ceux de
 * portée `global` vivent dans le layout racine, les autres sont
 * enregistrés par le composant qui porte l'action, et n'existent donc
 * que sur l'écran concerné. `scope` sert à le dire à l'utilisateur.
 */
export interface ShortcutDoc {
  /** Touche ou séquence, au format attendu par useKeyboardShortcuts. */
  keys: Shortcut['keys'];
  /** Libellé de l'action dans l'écran d'aide. */
  labelKey: MessageKey;
  /** Où le raccourci s'applique. */
  scope: 'global' | 'webhook';
}

export const KEY_HELP = '?';
export const KEY_GOTO_PAYMENTS: [string, string] = ['g', 'p'];
export const KEY_GOTO_SUBSCRIPTIONS: [string, string] = ['g', 's'];
export const KEY_GOTO_PAYMENT_METHODS: [string, string] = ['g', 'm'];

/**
 * Rejeu du webhook affiché — actif sur sa page de détail seulement.
 *
 * Aucun raccourci ne déclenche la réinitialisation générale : un
 * raccourci n'est pas découvrable, il faut le connaître pour s'en
 * servir. Sur une action qui vide la base, il ajoute du risque sans
 * ajouter d'accès — le bouton du Header est déjà à un clic.
 */
export const KEY_REPLAY_WEBHOOK = 'r';

export const SHORTCUT_DOCS: ShortcutDoc[] = [
  { keys: KEY_HELP, labelKey: 'shortcuts.help', scope: 'global' },
  { keys: KEY_GOTO_PAYMENTS, labelKey: 'shortcuts.gotoPayments', scope: 'global' },
  { keys: KEY_GOTO_SUBSCRIPTIONS, labelKey: 'shortcuts.gotoSubscriptions', scope: 'global' },
  { keys: KEY_GOTO_PAYMENT_METHODS, labelKey: 'shortcuts.gotoPaymentMethods', scope: 'global' },
  { keys: KEY_REPLAY_WEBHOOK, labelKey: 'shortcuts.replayWebhook', scope: 'webhook' },
];
