import { describe, expect, it, vi } from 'vitest';
import { act, renderHook, screen, waitFor } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

const api = vi.hoisted(() => ({
  fetchPlatformEventTypes: vi.fn(async () => ({
    eventTypes: ['agents.run.failed.v1'],
    error: null,
  })),
  fetchEventTypeFields: vi.fn(async (eventType: string) => ({
    fields:
      eventType === 'agents.run.failed.v1'
        ? [
            {
              path: 'data.entity.state',
              group: 'Resource',
              description: 'The run state',
              enum: ['failed', 'idle'],
            },
            { path: 'koraagent', group: 'Event', description: 'The agent' },
          ]
        : [],
    error: null,
  })),
}));
vi.mock('../_services/triggers-api', () => api);

import { PlatformEventFields, useEventTypeFields } from './platform-event-fields';
import { addFilterValue } from './filter-rows-editor';

describe('useEventTypeFields', () => {
  it("loads the chosen event type's schema fields and their values", async () => {
    const { result } = renderHook(() => useEventTypeFields('agents.run.failed.v1'));
    await waitFor(() => expect(result.current).toHaveLength(2));
    expect(result.current[0]).toMatchObject({
      token: 'data.entity.state',
      enum: ['failed', 'idle'],
    });
    expect(api.fetchEventTypeFields).toHaveBeenCalledWith('agents.run.failed.v1');
  });

  it('loads nothing before an event type is chosen', () => {
    api.fetchEventTypeFields.mockClear();
    const { result } = renderHook(() => useEventTypeFields(''));
    expect(result.current).toEqual([]);
    expect(api.fetchEventTypeFields).not.toHaveBeenCalled();
  });
});

describe('PlatformEventFields', () => {
  it("offers an enum field's values for its filter", async () => {
    const onFiltersChange = vi.fn();
    renderWithProviders(
      <PlatformEventFields
        eventType="agents.run.failed.v1"
        fields={[
          {
            token: 'data.entity.state',
            description: 'The run state',
            group: 'Resource',
            enum: ['failed', 'idle'],
          },
        ]}
        filters={[{ field: 'data.entity.state', values: '' }]}
        ignoreOwnRuns
        onEventTypeChange={vi.fn()}
        onFiltersChange={onFiltersChange}
        onIgnoreOwnRunsChange={vi.fn()}
      />,
    );
    expect(await screen.findByLabelText('Filter 1 value choices')).toBeTruthy();
    await act(async () => {});
  });
});

describe('addFilterValue', () => {
  it('appends a value once', () => {
    expect(addFilterValue('', 'failed')).toBe('failed');
    expect(addFilterValue('failed', 'idle')).toBe('failed | idle');
    expect(addFilterValue('failed | idle', 'idle')).toBe('failed | idle');
  });
});
