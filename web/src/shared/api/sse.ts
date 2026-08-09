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
 * Le rattrapage ne couvre pas tout, contrairement à ce que ce
 * commentaire affirmait. `internal/bus` le dit explicitement : un client
 * qui revient avec un `Last-Event-ID` sorti du ring — ou après un
 * redémarrage serveur, où le ring est vide et les identifiants
 * repartent — « perdra des events, le front doit alors refetch un
 * snapshot complet via les endpoints REST ».
 *
 * Cette responsabilité n'était pas assumée : au retour, l'indicateur
 * repassait au vert et l'interface continuait d'afficher des entités
 * disparues, sans que rien ne la contredise. D'où `onReconnect`, qui
 * distingue un retour d'une première connexion — seul un retour justifie
 * de tout relire.
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
  /**
   * Appelé sur une reconnexion, jamais sur la première ouverture.
   *
   * La distinction est le point du mécanisme : au premier `open`, les
   * hooks de liste chargent déjà les collections. Un refetch de plus
   * ferait double emploi à chaque montage de l'application.
   */
  onReconnect?: () => void;
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

  // EventSource rappelle `onopen` à chaque connexion réussie, la
  // première comprise. Ce drapeau porte toute la distinction.
  let dejaConnecte = false;

  source.onopen = () => {
    opts.onStatusChange?.(true);
    if (dejaConnecte) {
      opts.onReconnect?.();
    }
    dejaConnecte = true;
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
