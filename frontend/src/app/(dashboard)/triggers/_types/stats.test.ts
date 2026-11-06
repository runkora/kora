import { describe, expect, it } from 'vitest';

import { makeTrigger } from './fixtures';
import { needsAttention, successRate } from './stats';

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
    expect(successRate({ started24h: 0, failed24h: 0 })).toBeNull();
    expect(successRate({ started24h: 3, failed24h: 1 })).toBe(75);
  });
});
