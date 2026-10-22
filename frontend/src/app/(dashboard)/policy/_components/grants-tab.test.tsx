import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn(), push: vi.fn() }) }));
vi.mock('../_actions/grants', () => ({
  createGrantAction: vi.fn(),
  deleteGrantAction: vi.fn(),
  searchUsersAction: vi.fn(() => Promise.resolve({ ok: true, data: [] })),
}));

import { GrantsTab } from './grants-tab';
import type { Grant } from '../_types';

const grant = (
  id: string,
  principal_type: Grant['principal_type'],
  principal_id: string,
): Grant => ({
  id,
  tenant_id: 't',
  principal_type,
  principal_id,
  scopes: ['agents:read'],
  created_at: '2026-10-01T00:00:00Z',
});

const GRANTS: Grant[] = [
  grant('g1', 'HUMAN', 'usr_1'),
  grant('g2', 'AGENT', 'spiffe://orbit.test/tenant/acme/agent/ci-bot'),
  grant('g3', 'SERVICE', 'trigger:abc'),
  grant('g6', 'SERVICE', 'svc-1'),
  grant('g4', 'PAT', 'pat_1'),
  grant('g5', 'AGENT', 'a3f1c9e2-5b7d-4c1a-9e60-2d8b4f7a1c35'),
];

function renderTab(scopes = ['platform:grants:read', 'platform:grants:write']) {
  return renderWithProviders(
    <GrantsTab
      initialGrants={GRANTS}
      availableScopes={['agents:read']}
      patAttribution={{}}
      userAttribution={{}}
      initialError={null}
      page={1}
      limit={50}
      total={GRANTS.length}
    />,
    { scopes },
  );
}

describe('GrantsTab', () => {
  it('offers delete on a person grant only', () => {
    renderTab();
    expect(screen.getAllByRole('button', { name: 'Delete grant' })).toHaveLength(1);
  });

  it('links agent, trigger and token grants to their owner', () => {
    renderTab();
    expect(screen.getByRole('link', { name: 'Open agent' })).toHaveAttribute(
      'href',
      '/agents/ci-bot',
    );
    expect(screen.getByRole('link', { name: 'Open triggers' })).toHaveAttribute(
      'href',
      '/triggers',
    );
    expect(screen.getByRole('link', { name: 'Open API keys' })).toHaveAttribute(
      'href',
      '/model-gateway/keys',
    );
  });

  it('labels the SERVICE type "Trigger" and finds it by that word', () => {
    renderTab();
    expect(screen.getAllByText('Trigger').length).toBeGreaterThan(0);
    expect(screen.queryByText('SERVICE')).toBeNull();
    fireEvent.change(screen.getByPlaceholderText('Search grants...'), {
      target: { value: 'trigger' },
    });
    // svc-1 has no "trigger" in its ID, so only the type label can match it.
    expect(screen.getByText('svc-1')).toBeInTheDocument();
    expect(screen.queryByText('usr_1')).toBeNull();
  });

  it('in the list view offers delete on a person grant only, with owner links beside the rest', () => {
    renderTab();
    fireEvent.click(screen.getByRole('button', { name: 'List view' }));
    expect(screen.getAllByRole('button', { name: 'Delete grant' })).toHaveLength(1);
    expect(screen.getByRole('link', { name: 'Open agent' })).toHaveAttribute(
      'href',
      '/agents/ci-bot',
    );
    expect(screen.getByRole('link', { name: 'Open triggers' })).toHaveAttribute(
      'href',
      '/triggers',
    );
    expect(screen.getByRole('link', { name: 'Open API keys' })).toHaveAttribute(
      'href',
      '/model-gateway/keys',
    );
    // The UUID-keyed agent grant and the unparseable trigger grant stay read-only without a link.
    expect(screen.getAllByText('Read-only').length).toBe(2);
  });

  it('opens the human-only wizard from Create Grant', () => {
    renderTab();
    fireEvent.click(screen.getByRole('button', { name: /create grant/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.queryByRole('radio')).toBeNull();
    expect(screen.queryByRole('combobox')).toBeNull();
  });
});
