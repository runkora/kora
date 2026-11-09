import { describe, expect, it, vi } from 'vitest';
import { fireEvent, renderHook, screen, waitFor } from '@testing-library/react';
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
    error: null as string | null,
  })),
}));
vi.mock('../_services/triggers-api', () => api);

import { PlatformEventFields, useEventTypeFields } from './platform-event-fields';
import type { EventTypeFields } from './platform-event-fields';
import { addFilterValue } from './filter-rows-editor';

describe('useEventTypeFields', () => {
  it("loads the chosen event type's schema fields and their values", async () => {
    const { result } = renderHook(() => useEventTypeFields('agents.run.failed.v1'));
    expect(result.current.ready).toBe(false);
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.fields[0]).toMatchObject({
      token: 'data.entity.state',
      enum: ['failed', 'idle'],
    });
    expect(result.current.error).toBeNull();
    expect(api.fetchEventTypeFields).toHaveBeenCalledWith('agents.run.failed.v1');
  });

  it('reports a failed load', async () => {
    api.fetchEventTypeFields.mockResolvedValueOnce({ fields: [], error: 'kora-events is down' });
    const { result } = renderHook(() => useEventTypeFields('agents.run.failed.v1'));
    await waitFor(() => expect(result.current.error).toBe('kora-events is down'));
  });

  it('loads nothing before an event type is chosen', () => {
    api.fetchEventTypeFields.mockClear();
    const { result } = renderHook(() => useEventTypeFields(''));
    expect(result.current).toEqual({ fields: [], ready: false, error: null });
    expect(api.fetchEventTypeFields).not.toHaveBeenCalled();
  });
});

const STATE_FIELD = {
  token: 'data.entity.state',
  description: 'The run state',
  group: 'Resource',
  enum: ['failed', 'idle'],
};

function renderFields(fields: EventTypeFields, onFiltersChange = vi.fn()) {
  renderWithProviders(
    <PlatformEventFields
      eventType="agents.run.failed.v1"
      fields={fields}
      filters={[{ field: 'data.entity.state', values: '' }]}
      ignoreOwnRuns
      onEventTypeChange={vi.fn()}
      onFiltersChange={onFiltersChange}
      onIgnoreOwnRunsChange={vi.fn()}
    />,
  );
  return onFiltersChange;
}

describe('PlatformEventFields', () => {
  it("offers an enum field's values, and choosing one fills the filter", async () => {
    const onFiltersChange = renderFields({ fields: [STATE_FIELD], ready: true, error: null });
    fireEvent.click(await screen.findByLabelText('Filter 1 value choices'));
    fireEvent.click(await screen.findByText('failed'));
    expect(onFiltersChange).toHaveBeenCalledWith([
      { field: 'data.entity.state', values: 'failed' },
    ]);
  });

  it('says so when the fields fail to load', () => {
    renderFields({ fields: [], ready: true, error: 'down' });
    expect(screen.getByText(/Could not load the fields of this event/)).toBeTruthy();
  });
});

describe('addFilterValue', () => {
  it('appends a value once', () => {
    expect(addFilterValue('', 'failed')).toBe('failed');
    expect(addFilterValue('failed', 'idle')).toBe('failed | idle');
    expect(addFilterValue('failed | idle', 'idle')).toBe('failed | idle');
  });
});
