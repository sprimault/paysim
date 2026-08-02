// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { DataTable, type Column } from '@/shared/ui/DataTable';

interface Row {
  id: string;
  name: string;
  amount: number;
}

const cols: Column<Row>[] = [
  { header: 'Nom', cell: (r) => r.name },
  { header: 'Montant', cell: (r) => r.amount, align: 'right' },
];

describe('<DataTable />', () => {
  it('rend headers et rows', () => {
    render(
      <DataTable
        columns={cols}
        rows={[{ id: '1', name: 'A', amount: 100 }, { id: '2', name: 'B', amount: 200 }]}
        rowKey={(r) => r.id}
      />,
    );
    expect(screen.getByText('Nom')).toBeInTheDocument();
    expect(screen.getByText('Montant')).toBeInTheDocument();
    expect(screen.getByText('A')).toBeInTheDocument();
    expect(screen.getByText('B')).toBeInTheDocument();
  });

  it('affiche emptyState quand rows vide', () => {
    render(
      <DataTable
        columns={cols}
        rows={[]}
        rowKey={(r) => r.id}
        emptyState={<div>Aucune donnée</div>}
      />,
    );
    expect(screen.getByText('Aucune donnée')).toBeInTheDocument();
  });

  it('affiche skeleton quand loading + rows vide', () => {
    const { container } = render(
      <DataTable columns={cols} rows={[]} rowKey={(r) => r.id} loading />,
    );
    // Skeleton = plusieurs lignes animées, on vérifie juste la
    // structure rendue plutôt que le contenu textuel.
    expect(container.querySelector('.rounded-panel')).toBeInTheDocument();
  });

  it('emptyState ignoré si loading', () => {
    render(
      <DataTable
        columns={cols}
        rows={[]}
        rowKey={(r) => r.id}
        loading
        emptyState={<div>Aucune donnée</div>}
      />,
    );
    expect(screen.queryByText('Aucune donnée')).not.toBeInTheDocument();
  });

  it('appelle rowKey pour la clé React', () => {
    const keys = new Set<string>();
    const rowKey = (r: Row) => {
      keys.add(r.id);
      return r.id;
    };
    render(
      <DataTable
        columns={cols}
        rows={[{ id: 'a', name: 'A', amount: 1 }, { id: 'b', name: 'B', amount: 2 }]}
        rowKey={rowKey}
      />,
    );
    expect(keys.has('a')).toBe(true);
    expect(keys.has('b')).toBe(true);
  });
});
