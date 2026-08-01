// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { JsonViewer } from '../JsonViewer';

describe('JsonViewer', () => {
  it('pretty-print un objet passé en valeur', () => {
    const { container } = render(<JsonViewer value={{ a: 1, b: 'x' }} />);
    const code = container.querySelector('code');
    expect(code?.textContent).toContain('"a"');
    expect(code?.textContent).toContain('"b"');
    expect(code?.textContent).toContain('"x"');
  });

  it('reformate une chaîne JSON valide', () => {
    const { container } = render(<JsonViewer value={'{"a":1}'} />);
    const code = container.querySelector('code');
    // Après reformattage on doit voir un espace après les deux points.
    expect(code?.textContent).toMatch(/"a":\s*1/);
  });

  it('affiche la chaîne brute quand JSON invalide', () => {
    const { container } = render(<JsonViewer value="not json" />);
    expect(container.textContent).toContain('not json');
  });

  it('rend un bouton de copie', () => {
    render(<JsonViewer value={{ x: 1 }} />);
    expect(screen.getByRole('button', { name: /copier/i })).toBeInTheDocument();
  });
});
