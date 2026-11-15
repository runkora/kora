import { describe, expect, it } from 'vitest';

import { makeTrigger } from './fixtures';
import {
  EMPTY_TRIGGER_STATS,
  firedPerBucket,
  needsAttention,
  parseStatsRange,
  rangeStart,
  statsWindowOf,
  successRate,
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
  it('draws started and failed only, and nothing for declined deliveries', () => {
    const busy = { start: '', started: 1, failed: 2, other: 5 };
    const declined = { start: '', started: 0, failed: 0, other: 9 };
    expect(firedPerBucket({ ...EMPTY_TRIGGER_STATS, buckets: [busy, declined] })).toEqual([3, 0]);
  });
});

describe('stats range', () => {
  it('reads 24h, 7d and all, falls back to 24h, and bounds all history by nothing', () => {
    expect(parseStatsRange('7d')).toBe('7d');
    expect(parseStatsRange('all')).toBe('all');
    expect(parseStatsRange('30d')).toBe('24h');
    expect(parseStatsRange(null)).toBe('24h');
    expect(statsWindowOf('all')).toBe('24h');
    const now = Date.parse('2026-11-06T12:00:00Z');
    expect(rangeStart('7d', now)).toBe('2026-10-30T12:00:00.000Z');
    expect(rangeStart('all', now)).toBeUndefined();
  });
});
