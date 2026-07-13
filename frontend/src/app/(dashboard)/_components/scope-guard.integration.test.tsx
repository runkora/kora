/**
 * Integration tests for ScopeGuard.
 *
 * Strategy:
 * - `./permissions-provider` is vi.mock'd so `useSession` returns controllable
 *   session state without needing a live PermissionsProvider or MSW.
 * - Tests verify: null render while loading, children when scope is held,
 *   forbidden message when scope is absent, and exact-match semantics.
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { Text } from '@orbit/terra';
import { ScopeGuard } from './scope-guard';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('./permissions-provider', () => ({
  useSession: vi
    .fn()
    .mockReturnValue({ scopes: [], user: null, loaded: false, devEnvironmentId: null }),
}));

import { useSession } from './permissions-provider';

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderGuard(scope: string) {
  return renderWithProviders(
    <ScopeGuard scope={scope}>
      <Text>Protected content</Text>
    </ScopeGuard>,
  );
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('ScopeGuard', () => {
  it('renders null while the session is not yet loaded', () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: null,
      loaded: false,
      devEnvironmentId: null,
    });
    const { container } = renderGuard('platform:grants:read');
    expect(container).toBeEmptyDOMElement();
  });

  it('renders children when the principal holds the required scope', () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: ['platform:grants:read'],
      user: null,
      loaded: true,
      devEnvironmentId: null,
    });
    renderGuard('platform:grants:read');
    expect(screen.getByText('Protected content')).toBeInTheDocument();
  });

  it('renders the Forbidden message when the required scope is absent', () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: null,
      loaded: true,
      devEnvironmentId: null,
    });
    renderGuard('platform:grants:read');
    expect(screen.getByText('Forbidden')).toBeInTheDocument();
    expect(screen.getByText('You do not have permission to view this page.')).toBeInTheDocument();
  });

  it('does not render children when the scope is absent', () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: null,
      loaded: true,
      devEnvironmentId: null,
    });
    renderGuard('platform:grants:read');
    expect(screen.queryByText('Protected content')).not.toBeInTheDocument();
  });

  it('scope check is exact-match: platform:grants:read does not satisfy platform:grants:write', () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: ['platform:grants:read'],
      user: null,
      loaded: true,
      devEnvironmentId: null,
    });
    renderGuard('platform:grants:write');
    expect(screen.getByText('Forbidden')).toBeInTheDocument();
    expect(screen.queryByText('Protected content')).not.toBeInTheDocument();
  });
});
