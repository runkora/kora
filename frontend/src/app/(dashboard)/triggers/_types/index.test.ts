import { describe, expect, it, vi } from 'vitest';

import {
  DELIVERY_EXCERPT_MAX,
  LIMITS,
  toTriggerUpdateRequest,
  DELIVERY_SUMMARY_MAX,
  deliveryDetail,
  deliveryDotStatus,
  deliveryDuration,
  deliveryStatusTone,
  deliverySummary,
  deliveryTriggerIdentity,
  deliveryTriggerLabel,
  relativeTime,
  shortId,
  dispatchDuration,
  isRedeliverable,
  failureSummary,
  matchesStatusFilter,
  type DeliveryRow,
} from './index';
import { makeDelivery } from './fixtures';

/** These tests want a bare arrival: verified, unnamed, at a known instant. */
function row(overrides: Partial<DeliveryRow> = {}): DeliveryRow {
  return makeDelivery({
    status: 'verified',
    triggerName: null,
    receivedAt: '2026-06-26T00:00:00Z',
    ...overrides,
  });
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

describe('shortId', () => {
  it('cuts an id to eight characters, because a wrapped uuid is what makes a row three lines tall', () => {
    expect(shortId('5c0f7a1e-2b44-4f9e-9d21-77aa0f9b1c33')).toBe('5c0f7a1e');
  });

  it('leaves an already-short id alone rather than padding or truncating it', () => {
    expect(shortId('trg_9')).toBe('trg_9');
  });
});

describe('relativeTime', () => {
  it('answers the question a timestamp does not', () => {
    const now = Date.parse('2026-07-29T04:30:00Z');
    vi.useFakeTimers();
    vi.setSystemTime(now);
    expect(relativeTime('2026-07-29T04:29:50Z')).toBe('just now');
    expect(relativeTime('2026-07-29T04:24:00Z')).toBe('6m ago');
    expect(relativeTime('2026-07-29T01:30:00Z')).toBe('3h ago');
    expect(relativeTime('2026-07-27T04:30:00Z')).toBe('2d ago');
    vi.useRealTimers();
  });

  it('says never rather than inventing a gap', () => {
    expect(relativeTime(null)).toBe('Never');
    expect(relativeTime('not-a-date')).toBe('Never');
  });
});

describe('deliveryDotStatus', () => {
  it('shows an unrecognised status as idle rather than inventing a verdict', () => {
    expect(deliveryDotStatus('something-else')).toBe('idle');
  });

  it('carries the known tones through unchanged', () => {
    expect(deliveryDotStatus('dispatched')).toBe('healthy');
    expect(deliveryDotStatus('failed')).toBe('error');
  });
});

describe('deliveryTriggerIdentity', () => {
  it('reports a name as a name', () => {
    expect(deliveryTriggerIdentity(row({ triggerName: 'nightly-report' }))).toEqual({
      kind: 'name',
      label: 'nightly-report',
    });
  });

  it('shortens an id rather than wrapping a uuid across the row', () => {
    expect(
      deliveryTriggerIdentity(row({ triggerId: '5c0f7a1e-2b44-4f9e-9d21-77aa0f9b1c33' })),
    ).toEqual({ kind: 'id', label: '5c0f7a1e' });
  });

  it('counts the rest of a fan-out beside the first name', () => {
    expect(
      deliveryTriggerIdentity(
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
          ],
        }),
      ),
    ).toEqual({ kind: 'name', label: 'gh-push +1' });
  });

  it('has nothing to name when an arrival matched no trigger', () => {
    expect(deliveryTriggerIdentity(row({ triggerId: null, runs: [] }))).toEqual({ kind: 'none' });
  });
});

describe('deliverySummary', () => {
  it('lifts the human message out of a wrapped failure chain', () => {
    const r = row({
      error:
        'start run: unexpected status from orbit-agents: status 500: {"code":"internal_error","message":"failed to start run"}',
    });
    expect(deliverySummary(r)).toEqual({ kind: 'error', text: 'failed to start run' });
  });

  it('keeps the priority chain deliveryDetail already established', () => {
    expect(deliverySummary(row({ runId: 'abcdef1234567890' }))).toEqual({
      kind: 'runId',
      text: 'run abcdef12',
    });
    expect(
      deliverySummary(
        row({
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
              error: null,
              createdAt: null,
            },
          ],
        }),
      ),
    ).toEqual({ kind: 'runs', text: '2 runs' });
  });

  it('flattens newlines, so a multi-line chain cannot hold a row open', () => {
    const r = row({ error: 'start run:\n  connection refused' });
    const summary = deliverySummary(r);
    expect(summary.kind).toBe('error');
    if (summary.kind !== 'none') expect(summary.text).not.toContain('\n');
  });

  it('clips anything longer than a line', () => {
    const summary = deliverySummary(row({ payloadExcerpt: 'x'.repeat(200) }));
    if (summary.kind !== 'none') {
      expect(summary.text).toHaveLength(DELIVERY_SUMMARY_MAX + 1); // +1 for the ellipsis glyph
    }
  });

  it('has nothing to say about an arrival that recorded nothing', () => {
    expect(deliverySummary(row())).toEqual({ kind: 'none' });
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
    // 'superseded' belongs with 'dropped': the queue discarded it in favour of
    // a newer arrival, so the work it carried never ran.
    for (const status of ['failed', 'denied', 'dropped', 'superseded']) {
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

describe('LIMITS', () => {
  it('stops the parallel ceiling where the backend does', () => {
    // maxParallelCeiling in internal/triggers/service.go, and the DB check
    // constraint behind it. A UI that offered 17 would collect a value the API
    // rejects on submit.
    expect(LIMITS.parallel.max).toBe(16);
  });

  it('is one object, so the wizard and the drawer cannot bound differently', () => {
    expect(LIMITS.queueDepth).toEqual({ min: 1, max: 500 });
    expect(LIMITS.timeout).toEqual({ min: 5, max: 600 });
  });
});

describe('toTriggerUpdateRequest', () => {
  it('maps the console vocabulary onto the wire in one place', () => {
    expect(
      toTriggerUpdateRequest({
        name: 'gh-push',
        promptTemplate: 'Review it',
        eventType: 'push',
        matchCondition: { action: ['opened'] },
        scheduleConfig: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: true },
        enabled: false,
        dedupeEnabled: true,
        autopauseEnabled: false,
        maxParallel: 4,
        overflow: 'wait',
        maxQueueDepth: 32,
        timeoutSeconds: 30,
      }),
    ).toEqual({
      name: 'gh-push',
      prompt_template: 'Review it',
      event_type: 'push',
      match_condition: { action: ['opened'] },
      schedule_config: { cron_expression: '0 2 * * *', timezone: 'UTC', catch_up: true },
      enabled: false,
      dedupe_enabled: true,
      autopause_enabled: false,
      max_parallel: 4,
      overflow: 'wait',
      max_queue_depth: 32,
      timeout_seconds: 30,
    });
  });

  it('omits what was not set, since PATCH leaves an absent field alone', () => {
    expect(toTriggerUpdateRequest({ name: 'renamed' })).toEqual({ name: 'renamed' });
  });

  it('sends a sender restriction whole', () => {
    expect(
      toTriggerUpdateRequest({ senderRestriction: { logins: [], associations: ['MEMBER'] } }),
    ).toEqual({ sender_restriction: { logins: [], associations: ['MEMBER'] } });
  });

  it('defaults catch-up rather than sending undefined', () => {
    expect(
      toTriggerUpdateRequest({ scheduleConfig: { cronExpression: '0 2 * * *', timezone: 'UTC' } }),
    ).toEqual({
      schedule_config: { cron_expression: '0 2 * * *', timezone: 'UTC', catch_up: false },
    });
  });
});
