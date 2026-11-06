import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { TenantOption } from '@runkora/server/auth-config';
import { TenantList } from './tenant-list';

function tenants(names: string[]): TenantOption[] {
  return names.map((name, i) => ({ id: `t-${i}`, displayName: name, active: i === 0 }));
}

const SIX = ['Platform', 'Data', 'Machine Learning', 'Payments', 'Search', 'Mobile'];

function renderList(list: TenantOption[]) {
  render(<TenantList tenants={list} switching={null} failed={false} onPick={() => {}} />);
}

describe('TenantList search', () => {
  it('shows no search field for six tenants', () => {
    renderList(tenants(SIX));
    expect(screen.queryByRole('textbox', { name: 'Search tenants' })).not.toBeInTheDocument();
  });

  it('filters rows by name, ignoring case and surrounding space, above six tenants', async () => {
    const user = userEvent.setup();
    renderList(tenants([...SIX, 'Security']));

    await user.type(screen.getByRole('textbox', { name: 'Search tenants' }), '  sEarch ');

    expect(screen.getByRole('button', { name: 'Search' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Security' })).not.toBeInTheDocument();
    expect(screen.queryByText('No tenants match.')).not.toBeInTheDocument();
  });

  it('says no tenants match when the search finds none', async () => {
    const user = userEvent.setup();
    renderList(tenants([...SIX, 'Security']));

    await user.type(screen.getByRole('textbox', { name: 'Search tenants' }), 'billing');

    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(screen.getByText('No tenants match.')).toBeInTheDocument();
  });
});
