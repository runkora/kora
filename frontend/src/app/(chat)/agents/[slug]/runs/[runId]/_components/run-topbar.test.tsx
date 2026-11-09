import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders as render } from '@runkora/test-utils';
import { RunTopbar } from './run-topbar';

let ctx: Record<string, unknown>;
vi.mock('../_context/run-context', () => ({ useRunContext: () => ctx }));

beforeEach(() => {
  ctx = {
    slug: 'reviewer',
    agentName: 'Reviewer',
    model: 'm',
    runId: 'run-12345678',
    runState: 'active',
    summary: null,
    startedBy: 'trigger',
    sources: [],
    delegated: false,
    canvasState: { isOpen: false, tabs: [], activeTabId: null },
    openCanvas: vi.fn(),
    spentUsd: 0,
    perRunLimitUsd: null,
    tokensUsed: 0,
    showAllCalls: false,
    setShowAllCalls: vi.fn(),
    traceUrl: null,
  };
});

describe('RunTopbar', () => {
  it('shows a link for each run source', () => {
    ctx.sources = [
      { kind: 'pull_request', url: 'https://github.com/o/r/pull/12', label: 'PR #12' },
      { kind: 'repository', url: 'https://github.com/o/r', label: 'o/r' },
    ];
    render(<RunTopbar />);
    expect(screen.getByRole('link', { name: /PR #12/ })).toHaveAttribute(
      'href',
      'https://github.com/o/r/pull/12',
    );
    expect(screen.getByRole('link', { name: /o\/r/ })).toBeInTheDocument();
  });

  it('shows no source links for a run without sources', () => {
    render(<RunTopbar />);
    expect(screen.queryByRole('link', { name: /opens in a new tab/ })).not.toBeInTheDocument();
  });
});
