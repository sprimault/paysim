// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useMemo } from 'react';
import { CopyButton } from './CopyButton';

interface JsonViewerProps {
  value: unknown;
  className?: string;
  maxHeight?: string; // ex. 'max-h-96'
}

type TokenType = 'key' | 'string' | 'number' | 'bool' | 'null' | 'punct' | 'raw';

interface Token {
  type: TokenType;
  text: string;
}

interface Rendered {
  pretty: string;
  tokens: Token[];
}

/**
 * JsonViewer — pretty-print + coloration syntaxique maison (~40 lignes).
 * On ne charge pas un highlighter tiers pour ne pas gonfler le bundle
 * embarqué dans le binaire Go. Rendu en <pre> mono, bouton copier en
 * absolute top-right.
 *
 * Reçoit soit une valeur JS déjà parsée, soit une chaîne JSON — dans
 * ce dernier cas on tente le parse pour repretty-printer, sinon on
 * affiche brut.
 */
export function JsonViewer({ value, className = '', maxHeight = 'max-h-96' }: JsonViewerProps) {
  const { pretty, tokens } = useMemo<Rendered>(() => {
    let normalized: unknown = value;
    if (typeof value === 'string') {
      try {
        normalized = JSON.parse(value);
      } catch {
        return { pretty: value, tokens: [{ type: 'raw', text: value }] };
      }
    }
    const p = JSON.stringify(normalized, null, 2);
    return { pretty: p, tokens: tokenize(p) };
  }, [value]);

  return (
    <div className={'relative ' + className}>
      <CopyButton
        value={pretty}
        className="absolute right-2 top-2 z-10 bg-white/80 dark:bg-zinc-900/80"
      />
      <pre
        className={
          'overflow-auto rounded-md border border-zinc-200 bg-zinc-50 p-3 font-mono text-xs ' +
          'leading-relaxed text-zinc-800 dark:border-zinc-800 dark:bg-zinc-950 dark:text-zinc-200 ' +
          maxHeight
        }
      >
        <code>
          {tokens.map((t, i) => (
            <span key={i} className={tokenClass(t.type)}>
              {t.text}
            </span>
          ))}
        </code>
      </pre>
    </div>
  );
}

// Tokenizer minimal — lit un JSON déjà pretty-printé (donc bien formé)
// et sépare les catégories qu'on veut colorer. Pas de gestion d'unicode
// escape ni de contexte complet — l'entrée sort de JSON.stringify, ça
// suffit.
function tokenize(src: string): Token[] {
  const tokens: Token[] = [];
  const re =
    /("(?:[^"\\]|\\.)*")(\s*:)?|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|(true|false)|(null)|([{}[\],])|(\s+)|([^\s{}[\],]+)/g;
  const push = (type: TokenType, text: string) => tokens.push({ type, text });
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) {
    if (m[1]) {
      push(m[2] ? 'key' : 'string', m[1]);
      if (m[2]) push('punct', m[2]);
    } else if (m[3]) {
      push('number', m[3]);
    } else if (m[4]) {
      push('bool', m[4]);
    } else if (m[5]) {
      push('null', m[5]);
    } else if (m[6]) {
      push('punct', m[6]);
    } else if (m[7]) {
      push('raw', m[7]);
    } else if (m[8]) {
      push('raw', m[8]);
    }
  }
  return tokens;
}

function tokenClass(t: TokenType): string {
  switch (t) {
    case 'key':
      return 'text-brand-700 dark:text-brand-300';
    case 'string':
      return 'text-emerald-700 dark:text-emerald-400';
    case 'number':
      return 'text-amber-700 dark:text-amber-400';
    case 'bool':
      return 'text-sky-700 dark:text-sky-400';
    case 'null':
      return 'text-rose-700 dark:text-rose-400';
    case 'punct':
      return 'text-zinc-500 dark:text-zinc-500';
    case 'raw':
    default:
      return '';
  }
}
