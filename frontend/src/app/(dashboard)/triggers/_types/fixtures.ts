import type { DeliveryRow, TriggerRow } from './index';

/**
 * Test-support row builders, imported only from .test files. Each returns a
 * complete row with neutral defaults so a test spells out only the fields it
 * is actually about.
 */

export function makeTrigger(overrides: Partial<TriggerRow> = {}): TriggerRow {
  return {
    id: 't1',
    kind: 'webhook',
    sourceId: 'src-1',
    name: 'gh-push',
    agentTypeId: 'at-1',
    agentSlug: 'coder',
    eventType: 'push',
    matchCondition: {},
    senderRestriction: { logins: [], associations: [] },
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
    ...overrides,
  };
}

export function makeDelivery(overrides: Partial<DeliveryRow> = {}): DeliveryRow {
  return {
    id: 'd1',
    kind: 'webhook',
    sourceId: 'src-1',
    eventType: 'push',
    status: 'dispatched',
    triggerId: null,
    triggerName: 'gh-push',
    runId: null,
    error: null,
    payloadExcerpt: null,
    firedBy: null,
    scheduledAt: null,
    reason: null,
    payloadBytes: null,
    payloadPurged: false,
    receivedAt: '2026-07-29T04:24:55Z',
    runs: [],
    ...overrides,
  };
}
