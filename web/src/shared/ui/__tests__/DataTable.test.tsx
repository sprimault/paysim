// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen } from '@testing-library/react';
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

/** Colonnes triables, pour les cas de tri et de pagination. */
const triables: Column<Row>[] = [
  { header: 'Nom', cell: (r) => r.name, sortValue: (r) => r.name },
  { header: 'Montant', cell: (r) => r.amount, align: 'right', sortValue: (r) => r.amount },
];

/** Contenu texte des cellules d'une colonne, dans l'ordre affiché. */
function colonne(index: number): string[] {
  return screen
    .getAllByRole('row')
    .slice(1)
    .map((tr) => tr.children[index].textContent ?? '');
}

// Une seule barre de pagination, en tête du bloc collant. Les getAll
// restent pour que l'assertion de compte reste possible dans le cas qui
// la vérifie.
const suivant = () => screen.getAllByRole('button', { name: 'Suivant' })[0];
const precedent = () => screen.getAllByRole('button', { name: 'Précédent' })[0];
const selecteur = () => screen.getAllByLabelText('Lignes par page')[0];

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

describe('<DataTable /> tri', () => {
  const rows: Row[] = [
    { id: '1', name: 'Bravo', amount: 300 },
    { id: '2', name: 'Alpha', amount: 100 },
    { id: '3', name: 'Charlie', amount: 200 },
  ];

  function renderTri() {
    return render(<DataTable columns={triables} rows={rows} rowKey={(r) => r.id} />);
  }

  it('sans clic, conserve l\'ordre fourni', () => {
    renderTri();
    expect(colonne(0)).toEqual(['Bravo', 'Alpha', 'Charlie']);
  });

  it('une colonne sans sortValue n\'est pas cliquable', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} />);
    expect(screen.queryByRole('button', { name: /Nom/ })).not.toBeInTheDocument();
  });

  it('premier clic trie par ordre croissant', () => {
    renderTri();
    fireEvent.click(screen.getByRole('button', { name: /Nom/ }));
    expect(colonne(0)).toEqual(['Alpha', 'Bravo', 'Charlie']);
  });

  it('deuxième clic inverse le sens', () => {
    renderTri();
    const th = screen.getByRole('button', { name: /Nom/ });
    fireEvent.click(th);
    fireEvent.click(th);
    expect(colonne(0)).toEqual(['Charlie', 'Bravo', 'Alpha']);
  });

  // Le troisième temps du cycle : sans lui, un clic malheureux perdrait
  // l'ordre naturel de la liste jusqu'au rechargement de l'écran.
  it('troisième clic rend l\'ordre fourni', () => {
    renderTri();
    const th = screen.getByRole('button', { name: /Nom/ });
    fireEvent.click(th);
    fireEvent.click(th);
    fireEvent.click(th);
    expect(colonne(0)).toEqual(['Bravo', 'Alpha', 'Charlie']);
  });

  // Un tri lexicographique classerait 100, 200, 300 correctement mais
  // placerait 1000 avant 300 — d'où le montant à quatre chiffres.
  it('trie les nombres numériquement, pas comme des chaînes', () => {
    render(
      <DataTable
        columns={triables}
        rows={[
          { id: '1', name: 'A', amount: 300 },
          { id: '2', name: 'B', amount: 1000 },
          { id: '3', name: 'C', amount: 90 },
        ]}
        rowKey={(r) => r.id}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /Montant/ }));
    expect(colonne(1)).toEqual(['90', '300', '1000']);
  });

  it('annonce le sens courant via aria-sort', () => {
    renderTri();
    const bouton = screen.getByRole('button', { name: /Nom/ });
    expect(screen.getAllByRole('columnheader')[0]).not.toHaveAttribute('aria-sort');
    fireEvent.click(bouton);
    expect(screen.getAllByRole('columnheader')[0]).toHaveAttribute('aria-sort', 'ascending');
    fireEvent.click(bouton);
    expect(screen.getAllByRole('columnheader')[0]).toHaveAttribute('aria-sort', 'descending');
  });

  // Le store mémoïse la liste : trier en place ferait diverger la donnée
  // de son affichage, et le prochain rendu partirait d'un ordre faux.
  it('ne mute pas le tableau reçu', () => {
    const fournies: Row[] = [...rows];
    render(<DataTable columns={triables} rows={fournies} rowKey={(r) => r.id} />);
    fireEvent.click(screen.getByRole('button', { name: /Nom/ }));
    expect(fournies.map((r) => r.name)).toEqual(['Bravo', 'Alpha', 'Charlie']);
  });
});

describe('<DataTable /> pagination', () => {
  const rows: Row[] = Array.from({ length: 7 }, (_, i) => ({
    id: String(i),
    name: `L${i}`,
    amount: i,
  }));

  it('sans pageSize, rend toutes les lignes', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} />);
    expect(colonne(0)).toHaveLength(7);
  });

  it('découpe en pages et navigue', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={3} />);
    expect(colonne(0)).toEqual(['L0', 'L1', 'L2']);
    fireEvent.click(suivant());
    expect(colonne(0)).toEqual(['L3', 'L4', 'L5']);
    fireEvent.click(suivant());
    expect(colonne(0)).toEqual(['L6']);
    fireEvent.click(precedent());
    expect(colonne(0)).toEqual(['L3', 'L4', 'L5']);
  });

  it('désactive les boutons en bout de course', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={3} />);
    expect(precedent()).toBeDisabled();
    expect(suivant()).toBeEnabled();
    fireEvent.click(suivant());
    fireEvent.click(suivant());
    expect(suivant()).toBeDisabled();
  });

  it('pas de barre quand tout tient sur une page', () => {
    render(<DataTable columns={cols} rows={rows.slice(0, 2)} rowKey={(r) => r.id} pageSize={3} />);
    expect(screen.queryAllByRole('button', { name: 'Suivant' })).toHaveLength(0);
  });

  // Un filtre qui raccourcit la liste ne doit pas laisser l'utilisateur
  // sur une page devenue vide.
  it('ramène à la dernière page existante quand la liste rétrécit', () => {
    const { rerender } = render(
      <DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={3} />,
    );
    fireEvent.click(suivant());
    fireEvent.click(suivant());
    expect(colonne(0)).toEqual(['L6']);
    rerender(
      <DataTable columns={cols} rows={rows.slice(0, 4)} rowKey={(r) => r.id} pageSize={3} />,
    );
    expect(colonne(0)).toEqual(['L3']);
  });

  // Trier depuis la page 3 sans revenir en page 1 afficherait la fin du
  // nouvel ordre : l'utilisateur croirait le tri cassé.
  it('un tri ramène en première page', () => {
    render(<DataTable columns={triables} rows={rows} rowKey={(r) => r.id} pageSize={3} />);
    fireEvent.click(suivant());
    expect(colonne(0)).toEqual(['L3', 'L4', 'L5']);
    fireEvent.click(screen.getByRole('button', { name: /Montant/ }));
    expect(colonne(0)).toEqual(['L0', 'L1', 'L2']);
  });

  it('affiche la plage et le total', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={3} />);
    expect(screen.getAllByText('1–3 sur 7')).not.toHaveLength(0);
    expect(screen.getAllByText('Page 1 / 3')).not.toHaveLength(0);
  });

  // Une seule barre, en tete du bloc collant. Elle etait dupliquee en
  // bas tant que celle du haut sortait de l'ecran ; depuis qu'elle reste
  // visible en defilant, la seconde n'a plus d'objet.
  it('rend une seule barre de pagination, en haut', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={3} />);
    expect(screen.getAllByRole('button', { name: 'Suivant' })).toHaveLength(1);
    expect(screen.getAllByText('Page 1 / 3')).toHaveLength(1);
    fireEvent.click(suivant());
    expect(screen.getByText('Page 2 / 3')).toBeInTheDocument();
    expect(colonne(0)).toEqual(['L3', 'L4', 'L5']);
  });

  // La barre de l'ecran est rendue dans le meme bloc que la pagination :
  // c'est ce qui les fait defiler ensemble sans mesurer de hauteur.
  it('rend la toolbar fournie au-dessus de la pagination', () => {
    render(
      <DataTable
        columns={cols}
        rows={rows}
        rowKey={(r) => r.id}
        pageSize={3}
        toolbar={<div data-testid="toolbar">filtres</div>}
      />,
    );
    const toolbar = screen.getByTestId('toolbar');
    const bloc = toolbar.parentElement;
    expect(bloc?.className).toContain('sticky');
    expect(bloc).toContainElement(screen.getByRole('button', { name: 'Suivant' }));
  });
});

describe('<DataTable /> choix du nombre de lignes', () => {
  const rows: Row[] = Array.from({ length: 30 }, (_, i) => ({
    id: String(i),
    name: `L${i}`,
    amount: i,
  }));

  it('propose les paliers et l\'affichage complet', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={10} />);
    const options = Array.from(selecteur().querySelectorAll('option')).map((o) => o.textContent);
    expect(options).toEqual(['10', '25', '50', '100', 'Tout']);
  });

  it('changer de palier change le nombre de lignes rendues', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={10} />);
    expect(colonne(0)).toHaveLength(10);
    fireEvent.change(selecteur(), { target: { value: '25' } });
    expect(colonne(0)).toHaveLength(25);
    expect(screen.getByText('1–25 sur 30')).toBeInTheDocument();
  });

  it('« Tout » rend la liste entière sans découpage', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={10} />);
    fireEvent.change(selecteur(), { target: { value: '0' } });
    expect(colonne(0)).toHaveLength(30);
    expect(screen.getByText('1–30 sur 30')).toBeInTheDocument();
    expect(screen.getByText('Page 1 / 1')).toBeInTheDocument();
  });

  // Sans cela, choisir « tout » supprimerait le sélecteur qui vient de
  // servir : plus aucun moyen de revenir à dix lignes.
  it('le sélecteur survit à « Tout »', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={10} />);
    fireEvent.change(selecteur(), { target: { value: '0' } });
    expect(selecteur()).toBeInTheDocument();
    fireEvent.change(selecteur(), { target: { value: '10' } });
    expect(colonne(0)).toHaveLength(10);
  });

  // On change la taille pour en voir plus ou moins autour de là où on
  // est, pas pour être renvoyé au début de la liste. Le sens 25 → 10 est
  // celui qui le prouve : un simple retour en page 1 donnerait L0 alors
  // que la ligne sous les yeux était L25.
  it('garde sous les yeux la première ligne affichée', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={25} />);
    fireEvent.click(suivant()); // lignes 25..29
    expect(colonne(0)[0]).toBe('L25');
    fireEvent.change(selecteur(), { target: { value: '10' } });
    expect(colonne(0)[0]).toBe('L20');
    expect(screen.getByText('Page 3 / 3')).toBeInTheDocument();
  });

  it('agrandir la page ne perd pas la liste', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={10} />);
    fireEvent.click(suivant()); // lignes 10..19
    fireEvent.change(selecteur(), { target: { value: '25' } });
    expect(colonne(0)).toHaveLength(25);
    expect(colonne(0)[0]).toBe('L0');
  });

  it('la taille initiale figure dans les paliers meme hors liste standard', () => {
    render(<DataTable columns={cols} rows={rows} rowKey={(r) => r.id} pageSize={3} />);
    const options = Array.from(selecteur().querySelectorAll('option')).map((o) => o.textContent);
    expect(options[0]).toBe('3');
    expect((selecteur() as HTMLSelectElement).value).toBe('3');
  });
});
