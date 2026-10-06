import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import type { AgentRunStats } from '@orbit/shared/api';
import { DurationHistogram } from './duration-histogram';

const NONE: AgentRunStats['durations'] = {
  median_ms: 0,
  p95_ms: 0,
  p99_ms: 0,
  longest_ms: 0,
  capped: false,
  buckets: Array<number>(10).fill(0),
  overflow: 0,
};

const label = () => screen.getByRole('img').getAttribute('aria-label') ?? '';

describe('DurationHistogram', () => {
  it('reads as no finished runs and shows a dash for the longest', () => {
    renderWithProviders(<DurationHistogram durations={NONE} finished={0} />);
    expect(screen.getByRole('img', { name: 'Run durations, no finished runs.' })).toBeTruthy();
    expect(screen.getByText('–')).toBeTruthy();
  });

  it('draws runs that all took no time without a p95 marker', () => {
    renderWithProviders(
      <DurationHistogram
        durations={{ ...NONE, buckets: [2, 0, 0, 0, 0, 0, 0, 0, 0, 0] }}
        finished={2}
      />,
    );
    expect(label()).toContain('2 finished runs');
    expect(label()).not.toContain('p95');
  });

  it('marks p95 and labels the overflow bar only when the range is capped', () => {
    const durations = {
      ...NONE,
      median_ms: 60_000,
      p95_ms: 200_000,
      p99_ms: 240_000,
      longest_ms: 900_000,
      buckets: [4, 6, 5, 3, 2, 1, 1, 0, 0, 1],
      overflow: 2,
    };
    const { unmount } = renderWithProviders(
      <DurationHistogram durations={{ ...durations, capped: true }} finished={25} />,
    );
    expect(label()).toContain('p95 3m 20s');
    expect(screen.getByText('>4m')).toBeTruthy();
    unmount();

    renderWithProviders(
      <DurationHistogram durations={{ ...durations, overflow: 0 }} finished={25} />,
    );
    expect(label()).not.toContain('p95');
    expect(screen.queryByText('>4m')).toBeNull();
  });
});
