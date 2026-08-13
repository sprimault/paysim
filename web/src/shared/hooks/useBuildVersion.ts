// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react';
import { apiGetJson } from '@/shared/api/client';

interface VersionResponse {
  hash: string;
}

const POLL_INTERVAL_MS = 30_000;

/**
 * useBuildVersion interroge GET /paysim/api/v1/version au mount puis
 * périodiquement. Retourne `updateAvailable=true` quand le hash a
 * changé depuis la première réponse — signal qu'un nouveau bundle a
 * été déployé et qu'un reload chargera la nouvelle UI.
 *
 * La comparaison se fait par rapport à la première valeur reçue (pas à
 * la précédente) : on veut détecter tout écart avec la version qu'a
 * chargée le navigateur, pas seulement la toute dernière transition.
 * Une fois vrai, ne redevient jamais faux — c'est un latch.
 */
export function useBuildVersion(): { updateAvailable: boolean } {
  const initialRef = useRef<string | null>(null);
  const [updateAvailable, setUpdateAvailable] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const check = async () => {
      try {
        const r = await apiGetJson<VersionResponse>('/version');
        if (cancelled) return;
        if (initialRef.current === null) {
          initialRef.current = r.hash;
          return;
        }
        if (r.hash !== initialRef.current) {
          setUpdateAvailable(true);
        }
      } catch {
        // 404 ou réseau injoignable : on ignore silencieusement — pas
        // de raison de bruiter l'UI si l'endpoint est indisponible.
      }
    };
    void check();
    const id = window.setInterval(check, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  return { updateAvailable };
}
