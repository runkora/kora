import { describe, expect, it } from 'vitest';

import {
  DELIVERY_EXCERPT_MAX,
  deliveryDetail,
  deliveryDuration,
  deliveryStatusTone,
  deliveryTriggerLabel,
  dispatchDuration,
  isRedeliverable,
  failureSummary,
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
    triggerName: null,
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
        {
          triggerId: 't1',
          triggerName: null,
          runId: 'r1',
          status: 'completed',
          error: null,
          createdAt: null,
        },
        {
          triggerId: 't2',
          triggerName: null,
          runId: 'r2',
          status: 'failed',
          error: 'boom',
          createdAt: null,
        },
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
      runs: [
        {
          triggerId: 't1',
          triggerName: null,
          runId: 'r1',
          status: 'completed',
          error: null,
          createdAt: null,
        },
      ],
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

describe('failureSummary', () => {
  it('lifts the message out of a trailing JSON error body', () => {
    expect(
      failureSummary(
        'start run: unexpected status from orbit-agents: status 500: {"code":"internal_error","message":"failed to start run"}',
      ),
    ).toBe('failed to start run');
  });

  it('falls back to the last link of the chain when there is no body', () => {
    expect(failureSummary('start run: connection refused')).toBe('connection refused');
  });

  it('returns the raw string when nothing can be extracted', () => {
    expect(failureSummary('boom')).toBe('boom');
  });
});

describe('deliveryTriggerLabel', () => {
  it('prefers the name denormalised onto the delivery', () => {
    expect(deliveryTriggerLabel(row({ triggerName: 'nightly-report', triggerId: 't1' }))).toBe(
      'nightly-report',
    );
  });

  it('falls back to the fan-out when the delivery carries no name', () => {
    expect(
      deliveryTriggerLabel(
        row({
          runs: [
            {
              triggerId: 't1',
              triggerName: 'gh-push',
              runId: null,
              status: 'dispatched',
              error: null,
              createdAt: null,
            },
          ],
        }),
      ),
    ).toBe('gh-push');
  });

  it('counts the rest when an arrival fanned out to several triggers', () => {
    expect(
      deliveryTriggerLabel(
        row({
          runs: [
            {
              triggerId: 't1',
              triggerName: 'gh-push',
              runId: null,
              status: 'dispatched',
              error: null,
              createdAt: null,
            },
            {
              triggerId: 't2',
              triggerName: 'gh-audit',
              runId: null,
              status: 'dispatched',
              error: null,
              createdAt: null,
            },
            {
              triggerId: 't3',
              triggerName: 'gh-lint',
              runId: null,
              status: 'dispatched',
              error: null,
              createdAt: null,
            },
          ],
        }),
      ),
    ).toBe('gh-push +2');
  });

  it('falls back to an id rather than showing nothing', () => {
    expect(deliveryTriggerLabel(row({ triggerId: 'trg_123' }))).toBe('trg_123');
    expect(
      deliveryTriggerLabel(
        row({
          runs: [
            {
              triggerId: 'trg_9',
              triggerName: null,
              runId: null,
              status: 'skipped',
              error: null,
              createdAt: null,
            },
          ],
        }),
      ),
    ).toBe('trg_9');
  });

  it('says nothing matched, which is a real outcome and not missing data', () => {
    expect(deliveryTriggerLabel(row({ triggerId: null, runs: [] }))).toBe('—');
  });
});

describe('dispatchDuration', () => {
  it('reports sub-second gaps in milliseconds', () => {
    expect(dispatchDuration('2026-06-26T00:00:00.000Z', '2026-06-26T00:00:00.250Z')).toBe('250ms');
  });

  it('reports longer gaps in seconds, to one decimal', () => {
    expect(dispatchDuration('2026-06-26T00:00:00Z', '2026-06-26T00:00:01.500Z')).toBe('1.5s');
  });

  it('reports no duration when nothing ever ran', () => {
    // A filtered or denied arrival never reached a run, and "0ms" would read as
    // suspiciously fast rather than as not-applicable.
    expect(dispatchDuration('2026-06-26T00:00:00Z', null)).toBe('—');
  });

  it('refuses to report a negative or unparseable gap', () => {
    expect(dispatchDuration('2026-06-26T00:00:05Z', '2026-06-26T00:00:00Z')).toBe('—');
    expect(dispatchDuration('not-a-date', '2026-06-26T00:00:00Z')).toBe('—');
  });
});

describe('deliveryDuration', () => {
  const runs = [
    {
      triggerId: 't1',
      triggerName: null,
      runId: 'r1',
      status: 'dispatched',
      error: null,
      createdAt: '2026-06-26T00:00:00.400Z',
    },
    {
      triggerId: 't2',
      triggerName: null,
      runId: 'r2',
      status: 'dispatched',
      error: null,
      createdAt: '2026-06-26T00:00:02Z',
    },
  ];

  it('reads the named trigger out of the fan-out', () => {
    expect(deliveryDuration(row({ runs }), 't2')).toBe('2.0s');
  });

  it('falls back to the first outcome when no trigger is named', () => {
    expect(deliveryDuration(row({ runs }), undefined)).toBe('400ms');
  });

  it('has nothing to report for an arrival that started nothing', () => {
    expect(deliveryDuration(row({ runs: [] }))).toBe('—');
  });
});

describe('isRedeliverable', () => {
  it('allows replay of outcomes a replay could recover', () => {
    for (const status of ['failed', 'denied', 'dropped']) {
      expect(isRedeliverable(status)).toBe(true);
    }
  });

  it('refuses replay of work that already succeeded', () => {
    // The backend would accept this one; the rule exists so a stray click
    // cannot run completed work a second time.
    expect(isRedeliverable('dispatched')).toBe(false);
  });

  it('refuses replay of arrivals that were never meant to run', () => {
    for (const status of ['verified', 'skipped', 'duplicate', 'missed', 'pending']) {
      expect(isRedeliverable(status)).toBe(false);
    }
  });
});
