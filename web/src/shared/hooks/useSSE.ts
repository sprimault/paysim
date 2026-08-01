// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react';
import { subscribeSSE, type SSEEvent } from '../api/sse';

/**
 * useSSE souscrit à un flux SSE au montage, ferme au démontage, et
 * expose l'état de connexion.
 *
 * Le callback `onEvent` est stocké dans un ref pour éviter de
 * ré-ouvrir la connexion à chaque re-render — le hook ne dépend que
 * du chemin. En pratique, chaque consommateur passe un onEvent qui
 * dispatche vers un store Zustand (voir entities/payment,
 * entities/webhook en 3c.3).
 */
export function useSSE(path: string, onEvent: (evt: SSEEvent) => void): { connected: boolean } {
  const [connected, setConnected] = useState(false);
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  useEffect(() => {
    const handle = subscribeSSE(path, {
      onEvent: (evt) => onEventRef.current(evt),
      onStatusChange: setConnected,
    });
    return () => handle.close();
  }, [path]);

  return { connected };
}
