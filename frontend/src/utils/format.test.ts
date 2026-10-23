import { describe, it, expect } from 'vitest';
import {
  fmtNum,
  fmtNumCompact,
  fmtCost,
  fmtDateAxis,
  fmtDate,
  fmtTime,
  fmtExpiry,
  fmtDateTime,
  fmtTokens,
  fmtUsd,
  isExpired,
  plural,
} from './format';

describe('fmtNum', () => {
  it('formats integers with locale separators', () => {
    expect(fmtNum(1234567)).toBe('1,234,567');
  });

  it('formats small numbers without separators', () => {
    expect(fmtNum(42)).toBe('42');
  });
});

describe('fmtNumCompact', () => {
  it('formats millions with two decimal places and M suffix', () => {
    expect(fmtNumCompact(3_450_000)).toBe('3.45M');
  });

  it('formats exact millions', () => {
    expect(fmtNumCompact(1_000_000)).toBe('1.00M');
  });

  it('formats thousands with one decimal place and K suffix', () => {
    expect(fmtNumCompact(1_200)).toBe('1.2K');
  });

  it('formats exact thousands', () => {
    expect(fmtNumCompact(1_000)).toBe('1.0K');
  });

  it('formats sub-thousand numbers with locale separators', () => {
    expect(fmtNumCompact(999)).toBe('999');
  });

  it('formats zero', () => {
    expect(fmtNumCompact(0)).toBe('0');
  });
});

describe('fmtCost', () => {
  it('returns em dash for null', () => {
    expect(fmtCost(null)).toBe('—');
  });

  it('returns em dash for undefined', () => {
    expect(fmtCost(undefined as unknown as null)).toBe('—');
  });

  it('returns $0.00 for zero', () => {
    expect(fmtCost(0)).toBe('$0.00');
  });

  it('returns <$0.0001 for very small positive cost', () => {
    expect(fmtCost(0.00001)).toBe('<$0.0001');
  });

  it('returns <$0.0001 for value just below threshold', () => {
    expect(fmtCost(0.00009)).toBe('<$0.0001');
  });

  it('formats normal costs to 4 decimal places', () => {
    expect(fmtCost(0.0023)).toBe('$0.0023');
  });

  it('formats cost at exactly $0.0001', () => {
    expect(fmtCost(0.0001)).toBe('$0.0001');
  });

  it('formats larger costs to 4 decimal places', () => {
    expect(fmtCost(1.5)).toBe('$1.5000');
  });
});

describe('fmtDateAxis', () => {
  it('formats a date string as short month + day', () => {
    const result = fmtDateAxis('2025-01-15');
    expect(result).toBe('Jan 15');
  });

  it('returns "Invalid Date" for an unparseable string', () => {
    expect(fmtDateAxis('not-a-date')).toBe('Invalid Date');
  });

  it('uses UTC midnight to avoid off-by-one errors', () => {
    // 2025-03-01 must not become Feb 28 due to timezone shift
    const result = fmtDateAxis('2025-03-01');
    expect(result).toBe('Mar 1');
  });
});

describe('fmtDate', () => {
  it('formats an ISO timestamp as a short date', () => {
    const result = fmtDate('2025-01-01T00:00:00.000Z');
    expect(typeof result).toBe('string');
    expect(result).toContain('2025');
    expect(result).toContain('Jan');
  });
});

describe('fmtTime', () => {
  it('formats an ISO timestamp as a time string', () => {
    const result = fmtTime('2025-01-01T12:34:56.000Z');
    expect(typeof result).toBe('string');
    // Should contain digits separated by colons
    expect(result).toMatch(/\d{1,2}:\d{2}:\d{2}/);
  });
});

describe('fmtExpiry', () => {
  it('returns "Never expires" for null', () => {
    expect(fmtExpiry(null)).toBe('Never expires');
  });

  it('returns "Never expires" for undefined', () => {
    expect(fmtExpiry(undefined)).toBe('Never expires');
  });

  it('returns "Never expires" for empty string', () => {
    expect(fmtExpiry('')).toBe('Never expires');
  });

  it('formats a valid ISO date string', () => {
    const result = fmtExpiry('2026-12-31T00:00:00.000Z');
    expect(typeof result).toBe('string');
    expect(result).toContain('2026');
  });
});

describe('fmtDateTime', () => {
  it('formats an ISO timestamp as date + time without year', () => {
    const result = fmtDateTime('2025-06-15T14:30:00.000Z');
    expect(typeof result).toBe('string');
    // Should not contain the year
    expect(result).not.toContain('2025');
  });
});

describe('fmtTokens', () => {
  it('formats billions with one decimal place and B suffix', () => {
    expect(fmtTokens(1_200_000_000)).toBe('1.2B');
  });

  it('formats exact billion', () => {
    expect(fmtTokens(1_000_000_000)).toBe('1.0B');
  });

  it('formats millions with one decimal place and M suffix', () => {
    expect(fmtTokens(3_500_000)).toBe('3.5M');
  });

  it('formats exact million', () => {
    expect(fmtTokens(1_000_000)).toBe('1.0M');
  });

  it('formats thousands with one decimal place and K suffix', () => {
    expect(fmtTokens(1_200)).toBe('1.2K');
  });

  it('formats exact thousand', () => {
    expect(fmtTokens(1_000)).toBe('1.0K');
  });

  it('formats sub-thousand with locale string', () => {
    expect(fmtTokens(500)).toBe('500');
  });

  it('formats zero', () => {
    expect(fmtTokens(0)).toBe('0');
  });
});

describe('fmtUsd', () => {
  it('formats a USD amount with $ prefix and 2 decimal places', () => {
    expect(fmtUsd(1234.56)).toBe('$1,234.56');
  });

  it('formats zero', () => {
    expect(fmtUsd(0)).toBe('$0.00');
  });

  it('rounds to 2 decimal places', () => {
    expect(fmtUsd(1.005)).toMatch(/^\$1\.(00|01)$/);
  });
});

describe('isExpired', () => {
  it('returns false for null (never expires)', () => {
    expect(isExpired(null)).toBe(false);
  });

  it('returns false for undefined (never expires)', () => {
    expect(isExpired(undefined)).toBe(false);
  });

  it('returns false for empty string (never expires)', () => {
    expect(isExpired('')).toBe(false);
  });

  it('returns true for a past timestamp', () => {
    expect(isExpired('2020-01-01T00:00:00.000Z')).toBe(true);
  });

  it('returns false for a future timestamp', () => {
    expect(isExpired('2099-01-01T00:00:00.000Z')).toBe(false);
  });
});

describe('plural', () => {
  it('uses the singular for exactly one', () => {
    expect(plural(1, 'scope', 'scopes')).toBe('1 scope');
  });

  it('uses the plural for zero and many', () => {
    expect(plural(0, 'scope', 'scopes')).toBe('0 scopes');
    expect(plural(3, 'day', 'days')).toBe('3 days');
  });
});
