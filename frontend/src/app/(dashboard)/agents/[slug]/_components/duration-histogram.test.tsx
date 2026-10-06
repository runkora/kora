import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import type { RunDurations } from '../../_lib/overview';
import { DurationHistogram } from './duration-histogram';

const NONE: RunDurations = {
  finished: 0,
  medianMs: 0,
  p95Ms: 0,
  p99Ms: 0,
  longestMs: 0,
  capped: false,
  buckets: Array<number>(10).fill(0),
  overflow: 0,
};

describe('DurationHistogram', () => {
  it('reads as no finished runs and shows a dash for the longest', () => {
    renderWithProviders(<DurationHistogram durations={NONE} />);
    expect(screen.getByRole('img', { name: 'Run durations, no finished runs.' })).toBeTruthy();
    expect(screen.getByText('–')).toBeTruthy();
  });

  it('draws runs that all took no time without a p95 marker', () => {
    renderWithProviders(
      <DurationHistogram
        durations={{ ...NONE, finished: 2, buckets: [2, 0, 0, 0, 0, 0, 0, 0, 0, 0] }}
      />,
    );
    const label = screen.getByRole('img').getAttribute('aria-label') ?? '';
    expect(label).toContain('2 finished runs');
    expect(label).not.toContain('p95');
  });
});
