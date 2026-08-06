// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { useNavigate } from 'react-router';
import { useKeyboardShortcuts } from '@/shared/hooks/useKeyboardShortcuts';
import {
  KEY_GOTO_PAYMENTS,
  KEY_GOTO_PAYMENT_METHODS,
  KEY_GOTO_SUBSCRIPTIONS,
  KEY_HELP,
} from '@/shared/model/shortcuts';

/**
 * Raccourcis de portée application : navigation entre les trois listes
 * et ouverture de l'aide. Branché une seule fois, dans le layout racine.
 *
 * Les actions rattachées à un écran ou à un bouton précis ne sont pas
 * ici : le rejeu d'un webhook et la réinitialisation générale sont
 * enregistrés par les composants qui les portent, pour qu'un raccourci
 * ne survive jamais à l'élément qu'il pilote.
 *
 * @returns L'état d'ouverture de l'aide, à passer à `ShortcutsHelp`.
 */
export function useAppShortcuts(): { helpOpen: boolean; closeHelp: () => void } {
  const navigate = useNavigate();
  const [helpOpen, setHelpOpen] = useState(false);

  useKeyboardShortcuts([
    { keys: KEY_HELP, run: () => setHelpOpen(true) },
    { keys: KEY_GOTO_PAYMENTS, run: () => void navigate('/') },
    { keys: KEY_GOTO_SUBSCRIPTIONS, run: () => void navigate('/subscriptions') },
    { keys: KEY_GOTO_PAYMENT_METHODS, run: () => void navigate('/payment-methods') },
  ]);

  return { helpOpen, closeHelp: () => setHelpOpen(false) };
}
