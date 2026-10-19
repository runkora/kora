import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn(), push: vi.fn() }) }));
vi.mock('../_actions/roles', () => ({ upsertRoleAction: vi.fn(), deleteRoleAction: vi.fn() }));

import { RolesTab } from './roles-tab';
import type { RoleEntry } from '../_types/api';

const ROLES: RoleEntry[] = [
  {
    id: 'r1',
    tenant_id: 't',
    role: 'platform-admin',
    scopes: ['platform:read'],
    created_at: '',
    reserved: true,
  },
  { id: 'r2', tenant_id: 't', role: 'engineer', scopes: ['agents:types:read'], created_at: '' },
];

function renderRoles(scopes: string[]) {
  return renderWithProviders(
    <RolesTab initialRoles={ROLES} availableScopes={[]} initialError={null} />,
    { scopes },
  );
}

describe('RolesTab permissions', () => {
  it('shows write controls disabled with a hint to a read-only caller', () => {
    renderRoles(['platform:roles:read']);
    expect(screen.getByRole('button', { name: /create role/i })).toBeDisabled();
    expect(screen.getByLabelText("You don't have permission to create roles.")).toBeTruthy();
    for (const edit of screen.getAllByRole('button', { name: 'Edit role' })) {
      expect(edit).toBeDisabled();
    }
  });

  it('enables write controls for a roles writer', () => {
    renderRoles(['platform:roles:read', 'platform:roles:write']);
    expect(screen.getByRole('button', { name: /create role/i })).toBeEnabled();
    for (const edit of screen.getAllByRole('button', { name: 'Edit role' })) {
      expect(edit).toBeEnabled();
    }
  });

  it('never offers edit on the reserved role', () => {
    renderRoles(['platform:roles:read', 'platform:roles:write']);
    expect(screen.getAllByRole('button', { name: 'Edit role' })).toHaveLength(1);
  });
});
