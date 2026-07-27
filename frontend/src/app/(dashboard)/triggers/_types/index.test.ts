import { describe, expect, it } from 'vitest';

import {
  DELIVERY_EXCERPT_MAX,
  deliveryDetail,
  deliveryStatusTone,
  matchesStatusFilter,
  type DeliveryRow,
} from './index';

function row(overrides: Partial<DeliveryRow> = {}): DeliveryRow {
  return {
    id: 'd1',
    kind: 'webhook',
    sourceId: 'src-1',
    eventType: 'push',
    status: 'verified',
    triggerId: null,
    runId: null,
    error: null,
    payloadExcerpt: null,
    firedBy: null,
    scheduledAt: null,
    reason: null,
    payloadBytes: null,
    payloadPurged: false,
    receivedAt: '2026-06-26T00:00:00Z',
    runs: [],
    ...overrides,
  };
}

describe('deliveryDetail priority chain', () => {
  it('shows fan-out run count when there is more than one run (highest priority)', () => {
    const r = row({
      runs: [
        { triggerId: 't1', runId: 'r1', status: 'completed', error: null },
        { triggerId: 't2', runId: 'r2', status: 'failed', error: 'boom' },
      ],
      error: 'should-be-ignored',
      runId: 'r1',
      payloadExcerpt: 'ignored',
    });
    expect(deliveryDetail(r)).toEqual({ kind: 'runs', text: '2 runs' });
  });

  it('shows the error over run id and excerpt', () => {
    const r = row({ error: 'agent type not found', runId: 'r1', payloadExcerpt: 'ignored' });
    expect(deliveryDetail(r)).toEqual({ kind: 'error', text: 'agent type not found' });
  });

  it('shows a truncated run id over the excerpt', () => {
    const r = row({ runId: 'abcdef1234567890', payloadExcerpt: 'ignored' });
    expect(deliveryDetail(r)).toEqual({ kind: 'runId', text: 'run abcdef12' });
  });

  it('falls back to the payload excerpt when there is no run/error', () => {
    const r = row({ payloadExcerpt: '{"ref":"refs/heads/main"}' });
    expect(deliveryDetail(r)).toEqual({ kind: 'excerpt', text: '{"ref":"refs/heads/main"}' });
  });

  it('returns empty when nothing is present', () => {
    expect(deliveryDetail(row())).toEqual({ kind: 'empty' });
  });

  it('does not treat a single run as a fan-out (single run falls through to excerpt)', () => {
    const r = row({
      runs: [{ triggerId: 't1', runId: 'r1', status: 'completed', error: null }],
      payloadExcerpt: 'just-the-excerpt',
    });
    expect(deliveryDetail(r)).toEqual({ kind: 'excerpt', text: 'just-the-excerpt' });
  });
});

describe('deliveryDetail excerpt truncation', () => {
  it('leaves an excerpt of exactly the max length untruncated', () => {
    const exact = 'x'.repeat(DELIVERY_EXCERPT_MAX);
    expect(deliveryDetail(row({ payloadExcerpt: exact }))).toEqual({
      kind: 'excerpt',
      text: exact,
    });
  });

  it('truncates an over-length excerpt to max chars plus an ellipsis', () => {
    const long = 'x'.repeat(DELIVERY_EXCERPT_MAX + 50);
    const detail = deliveryDetail(row({ payloadExcerpt: long }));
    expect(detail.kind).toBe('excerpt');
    if (detail.kind === 'excerpt') {
      expect(detail.text).toBe(`${'x'.repeat(DELIVERY_EXCERPT_MAX)}…`);
      expect(detail.text).toHaveLength(DELIVERY_EXCERPT_MAX + 1); // +1 for the ellipsis glyph
    }
  });
});

describe('matchesStatusFilter (client-side OR semantics)', () => {
  it('keeps a row whose status is the single selected status', () => {
    expect(matchesStatusFilter(row({ status: 'failed' }), new Set(['failed']))).toBe(true);
  });

  it('drops a row whose status is not selected', () => {
    expect(matchesStatusFilter(row({ status: 'verified' }), new Set(['failed']))).toBe(false);
  });

  it('keeps rows matching any of multiple selected statuses (OR)', () => {
    const selected = new Set(['dispatched', 'failed']);
    expect(matchesStatusFilter(row({ status: 'dispatched' }), selected)).toBe(true);
    expect(matchesStatusFilter(row({ status: 'failed' }), selected)).toBe(true);
    expect(matchesStatusFilter(row({ status: 'skipped' }), selected)).toBe(false);
  });

  it('drops everything against an empty selection (DataTable skips filtering, so this path is the safe default)', () => {
    expect(matchesStatusFilter(row({ status: 'failed' }), new Set())).toBe(false);
  });
});

describe('deliveryStatusTone', () => {
  it.each([
    ['dispatched', 'healthy'],
    ['verified', 'degraded'],
    ['skipped', 'degraded'],
    ['failed', 'error'],
    ['something-else', 'unknown'],
  ])('maps %s -> %s', (status, tone) => {
    expect(deliveryStatusTone(status)).toBe(tone);
  });
});
