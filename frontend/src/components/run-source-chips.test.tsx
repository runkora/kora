import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Stack } from '@runkora/hades';
import type { RunSource } from '@runkora/shared/api';

import { RunSourceChips } from './run-source-chips';

const slack: RunSource = {
  kind: 'trigger',
  url: 'https://app.slack.com/client/T1/C1/thread/C1-1.2',
  label: 'Slack thread',
  provider: 'slack',
};
const pr: RunSource = {
  kind: 'trigger',
  url: 'https://github.com/o/r/pull/12',
  label: 'PR #12',
  provider: 'github',
};
const repo: RunSource = {
  kind: 'repository',
  url: 'https://github.com/o/r',
  label: 'o/r',
  provider: 'github',
};
const branch: RunSource = {
  kind: 'branch',
  url: 'https://github.com/o/r/tree/main',
  label: 'branch main',
  provider: 'github',
};

describe('RunSourceChips', () => {
  it('shows one chip per delivery and folds the rest into +N', () => {
    render(<RunSourceChips sources={[pr, repo, branch]} />);
    expect(screen.getAllByRole('link')).toHaveLength(1);
    expect(screen.getByRole('link', { name: /PR #12/ })).toHaveAttribute('href', pr.url);
    expect(screen.getByRole('button', { name: '2 more links' })).toHaveTextContent('+2');
  });

  it('lists the folded links in the popover', async () => {
    render(<RunSourceChips sources={[pr, repo, branch]} />);
    await userEvent.click(screen.getByRole('button', { name: '2 more links' }));
    expect(await screen.findByRole('link', { name: /o\/r/ })).toHaveAttribute('href', repo.url);
    expect(screen.getByRole('link', { name: /branch main/ })).toBeInTheDocument();
  });

  it('caps delivery chips at max', () => {
    render(<RunSourceChips sources={[slack, pr]} max={1} />);
    expect(screen.getAllByRole('link')).toHaveLength(1);
    expect(screen.getByRole('button', { name: '1 more link' })).toBeInTheDocument();
  });

  it('shows the first link when no delivery link was recorded', () => {
    render(<RunSourceChips sources={[repo]} />);
    expect(screen.getByRole('link', { name: /o\/r/ })).toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('opens links safely in a new tab', () => {
    render(<RunSourceChips sources={[slack, pr]} />);
    for (const link of screen.getAllByRole('link')) {
      expect(link).toHaveAttribute('target', '_blank');
      expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    }
  });

  it('keeps a chip click from reaching the row', () => {
    const onRow = vi.fn();
    render(
      <Stack onClick={onRow}>
        <RunSourceChips sources={[pr]} />
      </Stack>,
    );
    fireEvent.click(screen.getByRole('link', { name: /PR #12/ }));
    expect(onRow).not.toHaveBeenCalled();
  });

  it('renders nothing for an empty, null or missing list', () => {
    for (const s of [[], null, undefined]) {
      const { container, unmount } = render(<RunSourceChips sources={s} />);
      expect(container.firstChild).toBeNull();
      unmount();
    }
  });

  it('drops sources whose URL is not http(s)', () => {
    render(
      <RunSourceChips
        sources={[
          { kind: 'trigger', url: 'javascript:alert(1)', label: 'bad' },
          { kind: 'trigger', url: 'not a url', label: 'bad2' },
          pr,
        ]}
      />,
    );
    expect(screen.getAllByRole('link')).toHaveLength(1);
    expect(screen.queryByText('bad')).toBeNull();
  });
});
