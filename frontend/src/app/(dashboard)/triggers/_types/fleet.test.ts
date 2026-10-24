import { describe, expect, it } from 'vitest';

import { fleetStats, lastFired, needsAttention, recentFailures, sparkline } from './fleet';
import type { DeliveryRow, DeliveryRunRow, TriggerRow } from './index';

const NOW = Date.parse('2026-07-28T12:00:00Z');
const HOUR = 60 * 60 * 1000;

function trigger(overrides: Partial<TriggerRow> = {}): TriggerRow {
  return {
    id: 't1',
    kind: 'webhook',
    sourceId: 'src-1',
    declared: false,
    name: 'Trigger',
    targetKind: 'agent',
    agentTypeId: 'at-1',
    agentSlug: 'coder',
    routing: null,
    eventType: 'push',
    matchCondition: {},
    matchContains: {},
    senderRestriction: { ids: [], roles: [] },
    coalesce: null,
    promptTemplate: 'go',
    enabled: true,
    createdAt: '2026-07-01T00:00:00Z',
    maxParallel: 4,
    overflow: 'wait',
    maxQueueDepth: 32,
    timeoutSeconds: 30,
    dedupeEnabled: true,
    autopauseEnabled: true,
    consecutiveFailures: 0,
    autoPausedAt: null,
    pendingCount: 0,
    canFire: true,
    ...overrides,
  };
}

function delivery(ageHours: number, runs: Array<Partial<DeliveryRunRow>>): DeliveryRow {
  return {
    id: `d-${ageHours}-${Math.random()}`,
    kind: 'webhook',
    sourceId: 'src-1',
    eventType: 'push',
    status: 'dispatched',
    triggerId: 't1',
    triggerName: null,
    runId: null,
    error: null,
    payloadExcerpt: null,
    firedBy: null,
    scheduledAt: null,
    reason: null,
    payloadBytes: null,
    payloadPurged: false,
    receivedAt: new Date(NOW - ageHours * HOUR).toISOString(),
    runs: runs.map((r) => ({
      triggerId: 't1',
      triggerName: 'Trigger',
      runId: null,
      status: 'dispatched',
      error: null,
      createdAt: null,
      ...r,
    })),
    coalesceKeys: [],
  };
}

describe('fleetStats', () => {
  it('counts only deliveries from the last 24 hours', () => {
    const stats = fleetStats(
      [trigger()],
      [delivery(1, [{}]), delivery(23, [{}]), delivery(30, [{}])],
      NOW,
    );
    expect(stats.deliveries24h).toBe(2);
  });

  // Filtering is the configuration working, not a failure. Counting it would
  // make a correctly-configured fleet look unhealthy.
  it('excludes filtered and superseded outcomes from the success rate', () => {
    const stats = fleetStats(
      [trigger()],
      [
        delivery(1, [{ status: 'dispatched' }, { status: 'filtered' }]),
        delivery(2, [{ status: 'superseded' }]),
      ],
      NOW,
    );
    expect(stats.successRate).toBe(100);
  });

  it('counts denied and dropped as failures', () => {
    const stats = fleetStats(
      [trigger()],
      [delivery(1, [{ status: 'dispatched' }, { status: 'denied' }, { status: 'dropped' }])],
      NOW,
    );
    expect(stats.successRate).toBe(33);
  });

  // 0% would read as total failure where in fact nothing happened.
  it('reports no success rate rather than zero when nothing arrived', () => {
    expect(fleetStats([trigger()], [], NOW).successRate).toBeNull();
    expect(
      fleetStats([trigger()], [delivery(1, [{ status: 'filtered' }])], NOW).successRate,
    ).toBeNull();
  });

  it('counts active triggers separately from the total', () => {
    const stats = fleetStats([trigger(), trigger({ id: 't2', enabled: false })], [], NOW);
    expect(stats.activeTriggers).toBe(1);
    expect(stats.totalTriggers).toBe(2);
  });
});

describe('needsAttention', () => {
  it('flags an auto-paused trigger', () => {
    expect(needsAttention(trigger({ enabled: false, autoPausedAt: '2026-07-28T00:00:00Z' }))).toBe(
      true,
    );
  });

  // Warn before the pause, not after.
  it('flags a trigger part-way to auto-pause', () => {
    expect(needsAttention(trigger({ consecutiveFailures: 3 }))).toBe(true);
  });

  // Someone turned it off on purpose; that is not a problem to report.
  it('does not flag a deliberately disabled trigger', () => {
    expect(needsAttention(trigger({ enabled: false }))).toBe(false);
  });

  it('does not flag a healthy trigger', () => {
    expect(needsAttention(trigger())).toBe(false);
  });
});

describe('sparkline', () => {
  it('always returns a full set of buckets so a quiet trigger reads as quiet', () => {
    expect(sparkline('t1', [], NOW)).toHaveLength(12);
  });

  it('separates started, failed and other outcomes', () => {
    const buckets = sparkline(
      't1',
      [delivery(1, [{ status: 'dispatched' }, { status: 'failed' }, { status: 'filtered' }])],
      NOW,
    );
    const total = buckets.reduce(
      (acc, b) => ({
        started: acc.started + b.started,
        failed: acc.failed + b.failed,
        other: acc.other + b.other,
      }),
      { started: 0, failed: 0, other: 0 },
    );
    expect(total).toEqual({ started: 1, failed: 1, other: 1 });
  });

  it('ignores outcomes belonging to other triggers', () => {
    const d = delivery(1, [{ triggerId: 'other', status: 'dispatched' }]);
    const buckets = sparkline('t1', [d], NOW);
    expect(buckets.every((b) => b.started === 0)).toBe(true);
  });
});

describe('recentFailures', () => {
  it('counts only genuine failures in the window', () => {
    const rows = [
      delivery(1, [{ status: 'failed' }, { status: 'filtered' }]),
      delivery(2, [{ status: 'denied' }]),
      delivery(30, [{ status: 'failed' }]),
    ];
    expect(recentFailures('t1', rows, NOW)).toBe(2);
  });
});

describe('lastFired', () => {
  it('returns the most recent delivery for the trigger', () => {
    const older = delivery(5, [{}]);
    const newer = delivery(1, [{}]);
    expect(lastFired('t1', [older, newer])).toBe(newer.receivedAt);
  });

  it('returns null when the trigger has never fired', () => {
    expect(lastFired('nope', [delivery(1, [{}])])).toBeNull();
  });
});
