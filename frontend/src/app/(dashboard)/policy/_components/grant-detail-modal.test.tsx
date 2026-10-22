import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { Grant } from '../_types';
import { GrantDetailModal } from './grant-detail-modal';

const agentId = 'spiffe://orbit.test/tenant/orbit-tenant-example/agent/ci-failure-investigator';

const grant: Grant = {
  id: 'g1',
  tenant_id: 'example',
  principal_type: 'AGENT',
  principal_id: agentId,
  scopes: ['inference:invoke', 'connection:github:metadata:read', 'egress:github.com:connect'],
  budget: { limit_usd: 200, reset_cadence: 'weekly' } as Grant['budget'],
  created_at: '2026-09-29T00:00:00Z',
};

describe('GrantDetailModal', () => {
  it('renders nothing while no grant is selected', () => {
    render(<GrantDetailModal grant={null} identity={null} onClose={vi.fn()} />);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('shows the name, the full principal ID and every scope', () => {
    render(
      <GrantDetailModal
        grant={grant}
        identity={{ primary: 'ci-failure-investigator', rawId: agentId }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'ci-failure-investigator' })).toBeInTheDocument();
    expect(screen.getByText('AGENT')).toBeInTheDocument(); // the type badge beside the title
    expect(screen.queryByText('AGENT grant')).toBeNull(); // no generic subtitle
    expect(screen.getByText(agentId)).toBeInTheDocument();
    expect(screen.getByText('Scopes (3)')).toBeInTheDocument();
    expect(screen.getByText('github.com')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Copy principal ID' })).toBeInTheDocument();
  });

  it('says an agent grant is managed by its owner and links to it', () => {
    render(
      <GrantDetailModal
        grant={grant}
        identity={{ primary: 'ci-failure-investigator', rawId: agentId }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('Managed by')).toBeInTheDocument();
    expect(screen.getByText(/belongs to an agent and is read-only here/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open agent' })).toHaveAttribute(
      'href',
      '/agents/ci-failure-investigator',
    );
  });

  it('shows no managed-by section for a person', () => {
    render(
      <GrantDetailModal
        grant={{ ...grant, principal_type: 'HUMAN', principal_id: 'abc' }}
        identity={{ primary: 'Dev', rawId: 'abc' }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.queryByText('Managed by')).toBeNull();
    expect(screen.queryByRole('link')).toBeNull();
  });

  it('says so when there is no budget', () => {
    render(
      <GrantDetailModal
        grant={{ ...grant, budget: null }}
        identity={{ primary: 'ci-failure-investigator', rawId: agentId }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('No budget.')).toBeInTheDocument();
  });

  it('shows the email as the subtitle for a person', () => {
    render(
      <GrantDetailModal
        grant={{ ...grant, principal_type: 'HUMAN', principal_id: 'abc' }}
        identity={{ primary: 'Dev', email: 'dev@example.com', rawId: 'abc' }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('dev@example.com')).toBeInTheDocument();
    expect(screen.getByText('HUMAN')).toBeInTheDocument();
  });
});
