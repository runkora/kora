import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
vi.mock('../../../policy/_actions/grants', () => ({ updateGrantBudgetAction: vi.fn() }));

import { BudgetGrantsTab } from './budget-grants-tab';
import type { Grant } from '../../../policy/_types/api';

const grant = (id: string, principal_type: Grant['principal_type']): Grant => ({
  id,
  tenant_id: 't',
  principal_type,
  principal_id: `${principal_type.toLowerCase()}-1`,
  scopes: ['agents:read'],
  budget: { limit_usd: 10, reset_cadence: 'monthly' },
  created_at: '2026-10-01T00:00:00Z',
});

const GRANTS = [
  grant('g1', 'HUMAN'),
  grant('g2', 'AGENT'),
  grant('g3', 'SERVICE'),
  grant('g4', 'PAT'),
];

describe('BudgetGrantsTab', () => {
  it('offers the budget edit on a person grant only', () => {
    renderWithProviders(<BudgetGrantsTab grants={GRANTS} identities={{}} />, {
      scopes: ['platform:budgets:read', 'platform:budgets:write'],
    });
    expect(screen.getByRole('button', { name: 'Edit budget for human-1' })).toBeEnabled();
    expect(
      screen.queryByRole('button', { name: /Edit budget for (agent|service|pat)-1/ }),
    ).toBeNull();
  });

  it('finds a SERVICE grant by searching "trigger"', () => {
    const service = { ...grant('g3', 'SERVICE'), principal_id: 'svc-1' };
    renderWithProviders(
      <BudgetGrantsTab grants={[grant('g1', 'HUMAN'), service]} identities={{}} />,
      {
        scopes: ['platform:budgets:read'],
      },
    );
    fireEvent.change(screen.getByPlaceholderText('Search principals…'), {
      target: { value: 'trigger' },
    });
    expect(screen.getByText('svc-1')).toBeInTheDocument();
    expect(screen.queryByText('human-1')).toBeNull();
  });

  it('still shows the budget on a grant it cannot edit', () => {
    renderWithProviders(<BudgetGrantsTab grants={[grant('g2', 'AGENT')]} identities={{}} />, {
      scopes: ['platform:budgets:read', 'platform:budgets:write'],
    });
    expect(screen.getByText(/\$10/)).toBeInTheDocument();
  });
});
