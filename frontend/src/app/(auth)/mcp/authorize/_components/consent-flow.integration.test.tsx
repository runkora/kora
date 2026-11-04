/**
 * Integration tests for the tenant row on the MCP consent page: the Switch control shows only with
 * two or more tenants, and a switch returns to this consent request.
 */
import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import type { TenantOption } from '@runkora/server/auth-config';
import { ConsentFlow, type ConsentFlowProps } from './consent-flow';

vi.mock('@runkora/lib/tenant-switch', () => ({
  startTenantSwitch: vi.fn(),
  onTenantSwitch: () => () => {},
}));

import { startTenantSwitch } from '@runkora/lib/tenant-switch';

const PLATFORM: TenantOption = {
  id: 'kora-tenant-example',
  displayName: 'Example Platform',
  active: true,
};
const DATA: TenantOption = {
  id: 'kora-tenant-example-data',
  displayName: 'Example Data',
  active: false,
};
const CONSENT_PATH = '/mcp/authorize?client_id=cli&state=abc';

function renderConsent(tenants: TenantOption[]) {
  const props: ConsentFlowProps = {
    clientName: 'Claude Code',
    userEmail: 'dev@example.com',
    tenantName: 'Example Platform',
    tenants,
    consentPath: CONSENT_PATH,
    server: null,
    domains: [],
    grantedCount: 0,
    totalCount: 0,
    techRows: [],
    actionUrl: '/api/mcp/authorize',
    csrfToken: 'csrf',
  };
  return renderWithProviders(<ConsentFlow {...props} />);
}

describe('ConsentFlow tenant row', () => {
  it('shows the tenant name and no Switch control with one tenant', () => {
    renderConsent([PLATFORM]);
    expect(screen.getByText('Example Platform')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Switch tenant/ })).not.toBeInTheDocument();
  });

  it('switches tenant and returns to this consent request with two tenants', async () => {
    vi.mocked(startTenantSwitch).mockResolvedValue();
    const user = userEvent.setup();
    renderConsent([PLATFORM, DATA]);

    await user.click(screen.getByRole('button', { name: 'Switch tenant from Example Platform' }));
    await user.click(await screen.findByRole('button', { name: 'Example Data' }));

    expect(startTenantSwitch).toHaveBeenCalledWith({
      email: 'dev@example.com',
      tenantId: 'kora-tenant-example-data',
      returnPath: CONSENT_PATH,
    });
  });
});
