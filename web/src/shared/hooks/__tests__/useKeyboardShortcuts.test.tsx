// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useKeyboardShortcuts, type Shortcut } from '@/shared/hooks/useKeyboardShortcuts';

function Harness({ shortcuts, enabled }: { shortcuts: Shortcut[]; enabled?: boolean }) {
  useKeyboardShortcuts(shortcuts, enabled);
  return (
    <div>
      <input aria-label="champ" />
      <div>zone</div>
    </div>
  );
}

describe('useKeyboardShortcuts', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('déclenche une touche simple', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    render(<Harness shortcuts={[{ keys: '?', run }]} />);

    await user.keyboard('?');
    expect(run).toHaveBeenCalledOnce();
  });

  it('déclenche une séquence de deux touches', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    render(<Harness shortcuts={[{ keys: ['g', 'p'], run }]} />);

    await user.keyboard('gp');
    expect(run).toHaveBeenCalledOnce();
  });

  it('oublie le préfixe passé le délai', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    render(<Harness shortcuts={[{ keys: ['g', 'p'], run }]} />);

    await user.keyboard('g');
    vi.advanceTimersByTime(2000);
    await user.keyboard('p');
    expect(run).not.toHaveBeenCalled();
  });

  it('distingue la casse — R n’est pas r', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const minuscule = vi.fn();
    const majuscule = vi.fn();
    render(
      <Harness
        shortcuts={[
          { keys: 'r', run: minuscule },
          { keys: 'R', run: majuscule },
        ]}
      />,
    );

    await user.keyboard('{Shift>}R{/Shift}');
    expect(majuscule).toHaveBeenCalledOnce();
    expect(minuscule).not.toHaveBeenCalled();
  });

  it('reste inerte pendant une saisie', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    const { getByLabelText } = render(<Harness shortcuts={[{ keys: 'r', run }]} />);

    await user.click(getByLabelText('champ'));
    await user.keyboard('r');
    expect(run).not.toHaveBeenCalled();
  });

  it('reste inerte quand une modale est ouverte', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    render(<Harness shortcuts={[{ keys: 'r', run }]} />);

    const modale = document.createElement('div');
    modale.setAttribute('role', 'dialog');
    document.body.appendChild(modale);
    try {
      await user.keyboard('r');
      expect(run).not.toHaveBeenCalled();
    } finally {
      modale.remove();
    }
  });

  it('laisse passer les combinaisons avec Ctrl', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    render(<Harness shortcuts={[{ keys: 'r', run }]} />);

    await user.keyboard('{Control>}r{/Control}');
    expect(run).not.toHaveBeenCalled();
  });

  it('n’écoute plus quand enabled vaut false', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    render(<Harness shortcuts={[{ keys: 'r', run }]} enabled={false} />);

    await user.keyboard('r');
    expect(run).not.toHaveBeenCalled();
  });

  it('cesse d’écouter après démontage', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const run = vi.fn();
    const { unmount } = render(<Harness shortcuts={[{ keys: 'r', run }]} />);

    unmount();
    await user.keyboard('r');
    expect(run).not.toHaveBeenCalled();
  });
});
