import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { RunView } from '../_lib/run-view';
import { SessionsPane } from './sessions-pane';

const search = vi.hoisted(() => ({ params: new URLSearchParams() }));
vi.mock('next/navigation', () => ({ useSearchParams: () => search.params }));

const run = {
  id: 'run-1',
  agent: { name: 'Code reviewer', slug: 'code-reviewer' },
  durationMs: 0,
} as RunView;

const earlier = [{ id: 'run-0', label: 'An earlier run', meta: '3h', running: false }];

const renderPane = (query: string) => {
  search.params = new URLSearchParams(query);
  render(<SessionsPane run={run} rows={[]} earlier={earlier} earlierError={null} />);
};

describe('SessionsPane back link', () => {
  beforeEach(() => {
    search.params = new URLSearchParams();
  });

  it('returns to the agent when the run was not opened from the Runs page', () => {
    renderPane('');
    expect(screen.getByRole('link', { name: 'Code reviewer' })).toHaveAttribute(
      'href',
      '/agents/code-reviewer',
    );
    expect(screen.getByRole('link', { name: /An earlier run/ })).toHaveAttribute(
      'href',
      '/agents/code-reviewer/runs/run-0',
    );
  });

  it('returns to the Runs page and keeps the origin on other runs when opened from it', () => {
    renderPane('from=runs');
    expect(screen.getByRole('link', { name: 'Runs' })).toHaveAttribute('href', '/runs');
    expect(screen.getByRole('link', { name: /An earlier run/ })).toHaveAttribute(
      'href',
      '/agents/code-reviewer/runs/run-0?from=runs',
    );
  });

  it('returns to the Runs page with the filters it was opened under', () => {
    renderPane('from=runs&runs=quick%3Dmine%26q%3Dretry');
    expect(screen.getByRole('link', { name: 'Runs' })).toHaveAttribute(
      'href',
      '/runs?quick=mine&q=retry',
    );
    expect(screen.getByRole('link', { name: /An earlier run/ })).toHaveAttribute(
      'href',
      '/agents/code-reviewer/runs/run-0?from=runs&runs=quick%3Dmine%26q%3Dretry',
    );
  });

  it('ignores any other origin', () => {
    renderPane('from=https://example.com');
    expect(screen.getByRole('link', { name: 'Code reviewer' })).toHaveAttribute(
      'href',
      '/agents/code-reviewer',
    );
  });
});
