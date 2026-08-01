// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Tabs } from '../Tabs';

const tabs = [
  { id: 'a', label: 'Aperçu' },
  { id: 'b', label: 'Journal' },
  { id: 'c', label: 'Payload' },
];

describe('Tabs', () => {
  it('marque comme sélectionné le tab actif', () => {
    render(<Tabs tabs={tabs} active="b" onChange={() => undefined} />);
    expect(screen.getByRole('tab', { name: 'Journal' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByRole('tab', { name: 'Aperçu' })).toHaveAttribute(
      'aria-selected',
      'false',
    );
  });

  it('appelle onChange avec l\'id au clic', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Tabs tabs={tabs} active="a" onChange={onChange} />);
    await user.click(screen.getByRole('tab', { name: 'Payload' }));
    expect(onChange).toHaveBeenCalledWith('c');
  });

  it('affiche le badge quand fourni', () => {
    const withBadge = [{ id: 'a', label: 'A', badge: <span>42</span> }];
    render(<Tabs tabs={withBadge} active="a" onChange={() => undefined} />);
    expect(screen.getByText('42')).toBeInTheDocument();
  });
});
