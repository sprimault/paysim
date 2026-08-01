// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Radio, Zap } from 'lucide-react';
import { Link } from 'react-router';

interface HeaderProps {
  connected?: boolean; // état SSE, branché en 3c ; en 3b on affiche « démo »
}

export function Header({ connected = true }: HeaderProps) {
  return (
    <header className="sticky top-0 z-30 border-b border-zinc-200 bg-white/80 backdrop-blur dark:border-zinc-800 dark:bg-zinc-950/80">
      <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-6">
        <Link
          to="/"
          className="flex items-center gap-2 font-semibold tracking-tight text-zinc-900 dark:text-zinc-100"
        >
          <Zap size={18} className="text-brand-600 dark:text-brand-400" strokeWidth={2.5} />
          Paysim
        </Link>
        <ConnectionIndicator connected={connected} />
      </div>
    </header>
  );
}

function ConnectionIndicator({ connected }: { connected: boolean }) {
  return (
    <div
      className={
        'flex items-center gap-1.5 text-xs ' +
        (connected
          ? 'text-emerald-600 dark:text-emerald-400'
          : 'text-zinc-400 dark:text-zinc-600')
      }
      title={connected ? 'Flux SSE ouvert' : 'Flux SSE fermé'}
    >
      <Radio
        size={14}
        className={connected ? 'animate-pulse-slow' : ''}
        strokeWidth={connected ? 2.5 : 2}
      />
      <span className="hidden sm:inline">
        {connected ? 'Connecté' : 'Déconnecté'}
      </span>
    </div>
  );
}
