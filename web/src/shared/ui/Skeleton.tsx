// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

interface SkeletonProps {
  className?: string;
  count?: number;
}

/**
 * Placeholder de chargement — barre rectangulaire animée. Pas de shimmer
 * complexe (dev tool, on ne cherche pas à distraire). `count` répète le
 * bloc pour occuper une liste.
 */
export function Skeleton({ className = 'h-4 w-full', count = 1 }: SkeletonProps) {
  const items = Array.from({ length: count });
  return (
    <>
      {items.map((_, i) => (
        <div
          key={i}
          className={
            'animate-pulse rounded bg-zinc-200 dark:bg-zinc-800 ' +
            (i > 0 ? 'mt-2 ' : '') +
            className
          }
        />
      ))}
    </>
  );
}
