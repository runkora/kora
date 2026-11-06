import { describe, expect, it } from 'vitest';

import { makeTrigger } from './fixtures';
import {
  EMPTY_TRIGGER_STATS,
  firedPerBucket,
  isStatsWindow,
  needsAttention,
  successRate,
  windowStart,
} from './stats';

describe('needsAttention', () => {
  it('flags an auto-paused or failing trigger, not a deliberately disabled one', () => {
    expect(
      needsAttention(makeTrigger({ enabled: false, autoPausedAt: '2026-07-28T00:00:00Z' })),
    ).toBe(true);
    expect(needsAttention(makeTrigger({ consecutiveFailures: 3 }))).toBe(true);
    expect(needsAttention(makeTrigger({ enabled: false }))).toBe(false);
    expect(needsAttention(makeTrigger())).toBe(false);
  });
});

describe('successRate', () => {
  it('is null when nothing started or failed', () => {
    expect(successRate({ started: 0, failed: 0 })).toBeNull();
    expect(successRate({ started: 3, failed: 1 })).toBe(75);
  });
});

describe('firedPerBucket', () => {
  it('draws started and failed only, so a trigger that only declined draws nothing', () => {
    const bucket = (started: number, failed: number, other: number) => ({
      start: '',
      started,
      failed,
      other,
    });
    expect(
      firedPerBucket({ ...EMPTY_TRIGGER_STATS, buckets: [bucket(1, 2, 5), bucket(0, 0, 9)] }),
    ).toEqual([3, 0]);
  });
});

describe('stats window', () => {
  it('accepts 24h and 7d only, and starts the window that long before now', () => {
    expect(isStatsWindow('7d')).toBe(true);
    expect(isStatsWindow('30d')).toBe(false);
    expect(isStatsWindow('toString')).toBe(false);
    const now = Date.parse('2026-11-06T12:00:00Z');
    expect(windowStart('24h', now)).toBe('2026-11-05T12:00:00.000Z');
    expect(windowStart('7d', now)).toBe('2026-10-30T12:00:00.000Z');
  });
});
