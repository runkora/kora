import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { relativeTime } from './relative-time';

describe('relativeTime', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-11-05T12:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it.each([
    ['', 'never'],
    ['not a date', 'never'],
    ['2026-11-05T11:59:30Z', 'just now'],
    ['2026-11-05T11:46:00Z', '14m ago'],
    ['2026-11-05T09:00:00Z', '3h ago'],
    ['2026-11-02T12:00:00Z', '3d ago'],
    ['2026-11-05T12:05:00Z', 'just now'],
  ])('labels %s as %s', (iso, label) => {
    expect(relativeTime(iso)).toBe(label);
  });
});
