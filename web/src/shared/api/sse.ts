// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { apiUrl } from './basePath';

/**
 * Client SSE minimaliste au-dessus de l'EventSource natif.
 *
 * EventSource gère seul :
 *   - la reconnexion automatique (délai standard ~3s ajustable via
 *     `retry:` côté serveur, non utilisé pour l'instant) ;
 *   - la mémorisation et le renvoi de `Last-Event-ID` — c'est le
 *     mécanisme qui déclenche le catch-up côté handler Go
 *     (voir internal/api.streamEvents).
 *
 * Le contrat de non-doublon/non-trou est donc entièrement délégué au
 * couple navigateur + serveur, on n'a rien à dédupliquer côté client.
 */

// Format d'un event tel qu'écrit par api.streamEvents côté Go.
export interface SSEEvent {
  type: string;
  at: string; // RFC 3339
  data: unknown;
}

export interface SSEHandle {
  close(): void;
}

export interface SubscribeOptions {
  /** Appelé pour chaque événement reçu et parsé avec succès. */
  onEvent: (evt: SSEEvent) => void;
  /** Passe à `true` sur `open`, à `false` sur `error`. */
  onStatusChange?: (connected: boolean) => void;
}

/**
 * subscribeSSE ouvre une connexion EventSource sur le chemin donné
 * (préfixé par le base path Paysim) et retourne un handle avec
 * `close()`. Les lignes SSE mal formées (JSON invalide) sont
 * ignorées silencieusement — un event corrompu ne doit pas casser le
 * flux.
 */
export function subscribeSSE(path: string, opts: SubscribeOptions): SSEHandle {
  const source = new EventSource(apiUrl(path));

  source.onopen = () => {
    opts.onStatusChange?.(true);
  };

  source.onerror = () => {
    // EventSource entre en état "connecting" et retentera seul.
    // On notifie juste l'UI de l'état déconnecté.
    opts.onStatusChange?.(false);
  };

  source.onmessage = (msg: MessageEvent<string>) => {
    let parsed: SSEEvent;
    try {
      parsed = JSON.parse(msg.data) as SSEEvent;
    } catch {
      // Ligne mal formée — skip silencieux. Le flux continue.
      return;
    }
    opts.onEvent(parsed);
  };

  return {
    close: () => source.close(),
  };
}
