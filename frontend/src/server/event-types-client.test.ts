import { describe, expect, it, vi } from 'vitest';

vi.mock('@runkora/server/config', () => ({
  config: { koraEventsUrl: 'http://kora-events.test/v1' },
}));

import { fetchEventTypeSchema, schemaFields } from './event-types-client';

const SCHEMA = {
  properties: {
    type: { const: 'agents.run.failed.v1', description: 'The event type.' },
    koraagent: { type: 'string', description: 'The agent slug.' },
    data: {
      type: 'object',
      properties: {
        changed: { type: 'array', items: { type: 'string', enum: ['state', 'completed_at'] } },
        entity: {
          type: 'object',
          properties: {
            state: { type: 'string', enum: ['failed', 'idle'], description: 'The run state.' },
            level: { type: 'integer', enum: [50, 80, 100] },
          },
        },
      },
    },
  },
};

describe('schemaFields', () => {
  it('lists every path with its group, description and values', () => {
    const fields = schemaFields(SCHEMA);
    expect(fields.map((f) => f.path)).toEqual([
      'data',
      'data.changed',
      'data.entity',
      'data.entity.level',
      'data.entity.state',
      'koraagent',
      'type',
    ]);
    expect(fields.find((f) => f.path === 'data.entity.state')).toEqual({
      path: 'data.entity.state',
      group: 'Resource',
      description: 'The run state.',
      enum: ['failed', 'idle'],
    });
    expect(fields.find((f) => f.path === 'data.entity.level')?.enum).toEqual(['50', '80', '100']);
    expect(fields.find((f) => f.path === 'data.changed')?.enum).toEqual(['state', 'completed_at']);
    expect(fields.find((f) => f.path === 'data')?.group).toBe('Resource');
    expect(fields.find((f) => f.path === 'type')).toMatchObject({
      group: 'Event',
      enum: ['agents.run.failed.v1'],
    });
  });
});

describe('fetchEventTypeSchema', () => {
  it('reads the type schema from kora-events with the session token', async () => {
    const fetchMock = vi.fn(async () => Response.json(SCHEMA));
    vi.stubGlobal('fetch', fetchMock);
    await fetchEventTypeSchema('tok', 'agents.run.failed.v1');
    expect(fetchMock).toHaveBeenCalledWith(
      'http://kora-events.test/v1/events/types/agents.run.failed.v1',
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: 'Bearer tok' }),
      }),
    );
    vi.unstubAllGlobals();
  });
});

describe('fetchEventTypeSchema segment guard', () => {
  it.each(['', '.', '..'])('refuses %j before calling kora-events', async (bad) => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    await expect(fetchEventTypeSchema('tok', bad)).rejects.toMatchObject({ status: 404 });
    expect(fetchMock).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});
