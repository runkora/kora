import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { RunSource } from '@runkora/shared/api';

import { RunSourceLinks } from './run-source-links';

const sources: RunSource[] = [
  {
    kind: 'trigger',
    url: 'https://app.slack.com/client/T1/C1/thread/C1-1.2',
    label: 'Slack thread',
  },
  { kind: 'pull_request', url: 'https://github.com/o/r/pull/12', label: 'PR #12' },
  { kind: 'repository', url: 'https://github.com/o/r', label: 'o/r' },
  { kind: 'branch', url: 'https://github.com/o/r/tree/main', label: 'branch main' },
];

describe('RunSourceLinks', () => {
  it('renders one link per source with its href and label', () => {
    render(<RunSourceLinks sources={sources} />);
    const links = screen.getAllByRole('link');
    expect(links).toHaveLength(4);
    expect(links.map((l) => l.getAttribute('href'))).toEqual(sources.map((s) => s.url));
    expect(screen.getByText('PR #12')).toBeTruthy();
    expect(screen.getByRole('link', { name: /Slack thread/ })).toBeTruthy();
  });

  it('opens links safely in a new tab', () => {
    render(<RunSourceLinks sources={sources} />);
    for (const link of screen.getAllByRole('link')) {
      expect(link.getAttribute('target')).toBe('_blank');
      expect(link.getAttribute('rel')).toBe('noopener noreferrer');
    }
  });

  it('shows an icon for each kind', () => {
    const { container } = render(<RunSourceLinks sources={sources} />);
    expect(container.querySelectorAll('a svg')).toHaveLength(4);
  });

  it('renders nothing for an empty, null or missing list', () => {
    for (const s of [[], null, undefined]) {
      const { container, unmount } = render(<RunSourceLinks sources={s} />);
      expect(container.firstChild).toBeNull();
      unmount();
    }
  });

  it('drops sources whose URL is not http(s)', () => {
    render(
      <RunSourceLinks
        sources={[
          { kind: 'trigger', url: 'javascript:alert(1)', label: 'bad' },
          { kind: 'trigger', url: 'not a url', label: 'bad2' },
          sources[1],
        ]}
      />,
    );
    expect(screen.getAllByRole('link')).toHaveLength(1);
    expect(screen.queryByText('bad')).toBeNull();
  });
});
