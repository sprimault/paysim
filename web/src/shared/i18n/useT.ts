// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { messages, type MessageKey } from './messages';
import { useLangStore } from './store';

/**
 * useT retourne une fonction `t(key, params?)` qui résout une clé
 * dans le dictionnaire selon la langue courante. Re-render automatique
 * sur changement de langue via l'abonnement Zustand.
 *
 * Substitution : les placeholders `{param}` dans le message sont
 * remplacés par la valeur correspondante. Ex :
 *   messages.fr['payment.list.count'] = '{count} paiements en mémoire'
 *   t('payment.list.count', { count: 12 })  // → '12 paiements en mémoire'
 *
 * La clé est typée : une clé absente du dictionnaire fait échouer
 * le build TypeScript, pas seulement le runtime.
 */
export function useT(): (key: MessageKey, params?: Record<string, string | number>) => string {
  const lang = useLangStore((s) => s.lang);
  return (key, params) => {
    let msg = messages[lang][key] as string;
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        msg = msg.replace('{' + k + '}', String(v));
      }
    }
    return msg;
  };
}
