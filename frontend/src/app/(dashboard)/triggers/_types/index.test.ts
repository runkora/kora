/**
 * Unit tests for triggers _types/index.ts — toDeliveryRow transformer
 *
 * Covers:
 * - Correct mapping of payload_excerpt from API to DeliveryRow.payloadExcerpt
 * - Handling of null and undefined payload_excerpt values
 */
import { describe, it, expect } from 'vitest';
import { toDeliveryRow } from './index';
import type { TriggerDelivery } from '@orbit/shared/api';

describe('toDeliveryRow', () => {
  const baseTriggerDelivery: TriggerDelivery = {
    id: 'del-1',
    source_id: 'src-1',
    event_type: 'push',
    status: 'dispatched',
    trigger_id: 'trig-1',
    run_id: null,
    error: null,
    payload_excerpt: null,
    received_at: '2026-06-20T10:00:00Z',
    runs: [],
  };

  it('maps payload_excerpt to payloadExcerpt when present', () => {
    const delivery: TriggerDelivery = {
      ...baseTriggerDelivery,
      payload_excerpt: '{"repository": "orbit", "action": "opened"}',
    };

    const result = toDeliveryRow(delivery);

    expect(result.payloadExcerpt).toBe('{"repository": "orbit", "action": "opened"}');
  });

  it('maps null payload_excerpt to null payloadExcerpt', () => {
    const delivery: TriggerDelivery = {
      ...baseTriggerDelivery,
      payload_excerpt: null,
    };

    const result = toDeliveryRow(delivery);

    expect(result.payloadExcerpt).toBeNull();
  });

  it('maps undefined payload_excerpt to null payloadExcerpt', () => {
    const delivery: TriggerDelivery = {
      ...baseTriggerDelivery,
      payload_excerpt: undefined,
    };

    const result = toDeliveryRow(delivery);

    expect(result.payloadExcerpt).toBeNull();
  });

  it('preserves all other fields correctly alongside payloadExcerpt', () => {
    const delivery: TriggerDelivery = {
      id: 'del-123',
      source_id: 'src-456',
      event_type: 'pull_request',
      status: 'failed',
      trigger_id: 'trig-789',
      run_id: 'run-abc',
      error: 'Timeout error',
      payload_excerpt: '{"pr": 42}',
      received_at: '2026-06-21T15:30:00Z',
      runs: [
        {
          trigger_id: 'trig-789',
          run_id: 'run-abc',
          status: 'failed',
          error: 'Timeout',
        },
      ],
    };

    const result = toDeliveryRow(delivery);

    expect(result).toEqual({
      id: 'del-123',
      sourceId: 'src-456',
      eventType: 'pull_request',
      status: 'failed',
      triggerId: 'trig-789',
      runId: 'run-abc',
      error: 'Timeout error',
      payloadExcerpt: '{"pr": 42}',
      receivedAt: '2026-06-21T15:30:00Z',
      runs: [
        {
          triggerId: 'trig-789',
          runId: 'run-abc',
          status: 'failed',
          error: 'Timeout',
        },
      ],
    });
  });
});
