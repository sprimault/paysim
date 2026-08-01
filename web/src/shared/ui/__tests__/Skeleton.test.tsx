// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { Skeleton } from '../Skeleton';

describe('Skeleton', () => {
  it('rend un seul bloc par défaut', () => {
    const { container } = render(<Skeleton />);
    expect(container.querySelectorAll('.animate-pulse')).toHaveLength(1);
  });

  it('rend `count` blocs', () => {
    const { container } = render(<Skeleton count={4} />);
    expect(container.querySelectorAll('.animate-pulse')).toHaveLength(4);
  });
});
