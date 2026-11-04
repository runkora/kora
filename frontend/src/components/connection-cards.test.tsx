import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { ConnectionCatalogEntry } from '@runkora/shared/api';

import { ConnectionCards } from './connection-cards';

const AVAILABLE = [
  { name: 'github', adapter: 'github', actions: ['issues:read'], baseline: [] },
] as unknown as ConnectionCatalogEntry[];

describe('ConnectionCards read-only', () => {
  it('disables the grant checkboxes and keeps the expand control', () => {
    render(<ConnectionCards available={AVAILABLE} value={[]} onChange={vi.fn()} disabled />);
    expect(screen.getByRole('checkbox', { name: 'Grant all github actions' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Expand github actions' })).toBeEnabled();
  });
});
