import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import type { RunWithAttribution } from '@runkora/shared/api';
import type { RunsFeed } from '../_hooks/use-runs-feed';

const nav = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn(), search: '' }));
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: nav.push, replace: nav.replace }),
  usePathname: () => '/runs',
  useSearchParams: () => new URLSearchParams(nav.search),
}));
vi.mock('../../agents/_services/agents-api', () => ({
  fetchAgentTypes: () => Promise.resolve({ data: [], error: null }),
}));
const feed = vi.hoisted(() => ({ current: {} as RunsFeed }));
vi.mock('../_hooks/use-runs-feed', () => ({ useRunsFeed: () => feed.current }));

import { RunsPage } from './runs-page';

const run: RunWithAttribution = {
  id: 'run-1',
  tenant_id: 't',
  agent_type_id: 'agt',
  agent_name: 'Coder',
  agent_slug: 'coder',
  state: 'active',
  pod_name: '',
  created_by: 'usr',
  started_at: '2026-11-25T10:00:00Z',
  completed_at: null,
  created_at: '2026-11-25T10:00:00Z',
  updated_at: '2026-11-25T10:00:00Z',
  summary: 'Fix the flaky test',
};

const withFeed = (over: Partial<RunsFeed> = {}) => {
  feed.current = {
    runs: [],
    loading: false,
    error: null,
    retry: vi.fn(),
    pageIndex: 0,
    hasNewer: false,
    hasOlder: false,
    newer: vi.fn(),
    older: vi.fn(),
    pageLoading: false,
    pageError: null,
    freshCount: 0,
    showFresh: vi.fn(),
    reconnecting: false,
    liveIds: new Set(),
    ...over,
  };
};

beforeEach(() => {
  nav.push.mockReset();
  nav.replace.mockReset();
  nav.search = '';
  withFeed();
});

describe('RunsPage', () => {
  it('offers Clear filters when the filters match nothing, and clearing resets them', () => {
    renderWithProviders(<RunsPage />);
    expect(screen.getByText('No runs yet')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Filter by source'), { target: { value: 'webhook' } });
    expect(screen.getByText('No runs match these filters')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(screen.getByText('No runs yet')).toBeInTheDocument();
    expect(screen.getByLabelText('Filter by source')).toHaveValue('');
  });

  it('shows a failed first page as an error with a retry', () => {
    withFeed({ error: 'Could not load runs.' });
    renderWithProviders(<RunsPage />);
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(feed.current.retry).toHaveBeenCalled();
    expect(screen.getByText('Could not load runs')).toBeInTheDocument();
  });

  it('keeps loaded rows under a reload error and marks live runs New', () => {
    withFeed({ runs: [run], error: 'Could not load runs.', liveIds: new Set(['run-1']) });
    renderWithProviders(<RunsPage />);
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load runs.');
    expect(screen.getByText('Fix the flaky test')).toBeInTheDocument();
    expect(screen.getByText('New')).toBeInTheDocument();
  });

  it('opens a run marked as coming from the Runs page', () => {
    withFeed({ runs: [run] });
    renderWithProviders(<RunsPage />);
    fireEvent.click(screen.getByText('Fix the flaky test'));
    expect(nav.push).toHaveBeenCalledWith('/agents/coder/runs/run-1?from=runs');
  });

  it('restores the filters from the URL and carries them to the run it opens', () => {
    nav.search = 'quick=bogus&trigger_kind=webhook&q=flaky';
    withFeed({ runs: [run] });
    renderWithProviders(<RunsPage />);
    expect(screen.getByLabelText('Filter by source')).toHaveValue('webhook');
    expect(screen.getByPlaceholderText('Search runs…')).toHaveValue('flaky');
    // The unknown quick filter falls back to All, and the URL is rewritten without it.
    expect(nav.replace).toHaveBeenCalledWith('/runs?trigger_kind=webhook&q=flaky', {
      scroll: false,
    });
    fireEvent.click(screen.getByText('Fix the flaky test'));
    expect(nav.push).toHaveBeenCalledWith(
      '/agents/coder/runs/run-1?from=runs&runs=trigger_kind%3Dwebhook%26q%3Dflaky',
    );
  });

  it('writes a filter change to the URL with replace', () => {
    renderWithProviders(<RunsPage />);
    expect(nav.replace).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('Filter by source'), { target: { value: 'event' } });
    expect(nav.replace).toHaveBeenCalledWith('/runs?trigger_kind=event', { scroll: false });
    expect(nav.push).not.toHaveBeenCalled();
  });

  it('says when the live stream is reconnecting', () => {
    withFeed({ reconnecting: true });
    renderWithProviders(<RunsPage />);
    expect(screen.getByText(/live stream dropped and is reconnecting/)).toBeInTheDocument();
  });
});
