// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Radio, Zap } from 'lucide-react';
import { Tooltip } from '@/shared/ui/Tooltip';
import { Link, NavLink } from 'react-router';
import { LangToggle } from '@/shared/ui/LangToggle';
import { ResetAllButton } from '@/shared/ui/ResetAllButton';
import { ThemeToggle } from '@/shared/ui/ThemeToggle';
import { useT } from '@/shared/i18n/useT';
import { useNavCounts } from '@/widgets/header/model/useNavCounts';

interface HeaderProps {
  connected?: boolean;
}

export function Header({ connected = true }: HeaderProps) {
  const t = useT();
  const counts = useNavCounts();
  return (
    <header className="sticky top-0 z-30 border-b border-zinc-200 bg-white/80 backdrop-blur dark:border-zinc-800 dark:bg-zinc-950/80">
      <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-6">
        <div className="flex items-center gap-6">
          <Link
            to="/"
            className="flex items-center gap-2 font-semibold tracking-tight text-zinc-900 dark:text-zinc-100"
          >
            <Zap size={18} className="text-brand-600 dark:text-brand-400" strokeWidth={2.5} />
            Paysim
          </Link>
          <nav className="flex items-center gap-1 text-sm" aria-label={t('header.nav.aria')}>
            <NavItem to="/" end count={counts.payments}>
              {t('header.nav.payments')}
            </NavItem>
            <NavItem to="/subscriptions" count={counts.subscriptions}>
              {t('header.nav.subscriptions')}
            </NavItem>
            <NavItem to="/payment-methods" count={counts.paymentMethods}>
              {t('header.nav.paymentMethods')}
            </NavItem>
          </nav>
        </div>
        <div className="flex items-center gap-3">
          <ConnectionIndicator connected={connected} />
          <ThemeToggle />
          <LangToggle />
          {/* Séparé des sélecteurs par un filet : action destructive
              voisine de réglages anodins, la frontière doit se voir. */}
          <span className="mx-1 h-5 w-px bg-zinc-200 dark:bg-zinc-800" aria-hidden="true" />
          <ResetAllButton />
        </div>
      </div>
    </header>
  );
}

function NavItem({
  to,
  end,
  count,
  children,
}: {
  to: string;
  end?: boolean;
  count?: number;
  children: React.ReactNode;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        'flex items-center gap-1.5 rounded px-2.5 py-1 transition-colors ' +
        (isActive
          ? 'bg-brand-100 text-brand-800 dark:bg-brand-900/40 dark:text-brand-300'
          : 'text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100')
      }
    >
      {children}
      {/* Pastille masquée à zéro : sur un simulateur vide, trois « 0 »
          alignés ajoutent du bruit sans rien apprendre. */}
      {count !== undefined && count > 0 && (
        <span className="rounded-full bg-zinc-200 px-1.5 text-xs font-medium tabular-nums text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300">
          {count}
        </span>
      )}
    </NavLink>
  );
}

function ConnectionIndicator({ connected }: { connected: boolean }) {
  const t = useT();
  return (
    <Tooltip
      label={
        connected
          ? t('header.connection.titleConnected')
          : t('header.connection.titleDisconnected')
      }
    >
      <span
        className={
          'flex items-center gap-1.5 text-xs ' +
          (connected
            ? 'text-emerald-600 dark:text-emerald-400'
            : 'text-zinc-400 dark:text-zinc-600')
        }
      >
        <Radio
          size={14}
          className={connected ? 'animate-pulse-slow' : ''}
          strokeWidth={connected ? 2.5 : 2}
        />
        <span className="hidden sm:inline">
          {connected ? t('header.connection.connected') : t('header.connection.disconnected')}
        </span>
      </span>
    </Tooltip>
  );
}
