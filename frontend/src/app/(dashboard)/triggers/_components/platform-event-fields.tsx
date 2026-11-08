'use client';

/**
 * @tier 3 · triggers-scoped. The event, filters and own-run guard of an event
 * trigger. Used by the New trigger wizard and by a stored trigger's Configuration tab.
 */

import { useEffect, useState } from 'react';
import { SectionLabel, Stack, Text, ToggleRow } from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import { CatalogPicker } from './catalog-picker';
import { AddFilterButton, FilterRowsEditor } from './filter-rows-editor';
import type { FilterRow } from './filter-rows-editor';
import { eventTypeCatalog, schemaCatalog } from './trigger-wizard/field-catalog';
import type { SchemaCatalogEntry } from './trigger-wizard/field-catalog';
import { fetchEventTypeFields, fetchPlatformEventTypes } from '../_services/triggers-api';

/** Loads the platform event catalog once per mount. */
function usePlatformEventTypes(): { eventTypes: string[]; error: string | null } {
  const [answer, setAnswer] = useState<{ eventTypes: string[]; error: string | null }>({
    eventTypes: [],
    error: null,
  });
  useEffect(() => {
    let live = true;
    void fetchPlatformEventTypes().then((res) => {
      if (live) setAnswer(res);
    });
    return () => {
      live = false;
    };
  }, []);
  return answer;
}

/** One event type's fields, whether they have loaded, and why a load failed. */
export interface EventTypeFields {
  fields: SchemaCatalogEntry[];
  ready: boolean;
  error: string | null;
}

const NO_FIELDS: EventTypeFields = { fields: [], ready: false, error: null };

/** The fields of one event type and their allowed values, from its schema. */
export function useEventTypeFields(eventType: string): EventTypeFields {
  const [answer, setAnswer] = useState<EventTypeFields & { eventType: string }>({
    eventType: '',
    ...NO_FIELDS,
  });
  useEffect(() => {
    if (eventType === '') return;
    let live = true;
    void fetchEventTypeFields(eventType).then((res) => {
      if (live)
        setAnswer({ eventType, fields: schemaCatalog(res.fields), ready: true, error: res.error });
    });
    return () => {
      live = false;
    };
  }, [eventType]);
  if (eventType === '' || answer.eventType !== eventType) return NO_FIELDS;
  return { fields: answer.fields, ready: answer.ready, error: answer.error };
}

interface PlatformEventFieldsProps {
  eventType: string;
  /** The event type's fields, from `useEventTypeFields`. */
  fields: EventTypeFields;
  filters: FilterRow[];
  ignoreOwnRuns: boolean;
  onEventTypeChange: (eventType: string) => void;
  onFiltersChange: (rows: FilterRow[]) => void;
  onIgnoreOwnRunsChange: (ignoreOwnRuns: boolean) => void;
  disabled?: boolean;
}

export function PlatformEventFields({
  eventType,
  fields,
  filters,
  ignoreOwnRuns,
  onEventTypeChange,
  onFiltersChange,
  onIgnoreOwnRunsChange,
  disabled = false,
}: PlatformEventFieldsProps) {
  const { productName } = useBrand();
  const catalog = usePlatformEventTypes();
  return (
    <Stack gap="relaxed">
      <Stack gap="snug">
        <Stack gap="tight">
          <SectionLabel>Event</SectionLabel>
          <Text size="caption" color="subtle">
            {`The ${productName} event this trigger listens for. Only events in this tenant fire it.`}
          </Text>
        </Stack>
        <Stack direction="horizontal" gap="tight" align="center" paddingInline="snug">
          <Text as="span" size="supporting" color="muted">
            Listen for
          </Text>
          <CatalogPicker
            inline
            aria-label="Event type"
            value={eventType}
            placeholder="Choose an event"
            catalog={eventTypeCatalog(catalog.eventTypes, [])}
            searchPlaceholder="Search events…"
            onSelect={onEventTypeChange}
            disabled={disabled}
          />
        </Stack>
        {catalog.error !== null && (
          <Text size="caption" color="status-error">
            Could not load the event types. Close this and open it again to retry.
          </Text>
        )}
      </Stack>

      <Stack gap="snug">
        <Stack gap="tight">
          <SectionLabel>Filters</SectionLabel>
          <Text size="caption" color="subtle">
            Only events matching every filter start a run. Fields are dotted paths in the event,
            such as data.entity.state. Values are alternatives.
          </Text>
        </Stack>
        <FilterRowsEditor
          rows={filters}
          fields={fields.fields}
          onChange={onFiltersChange}
          freeText={fields.error !== null}
          hideAdd
          disabled={disabled}
        />
        <AddFilterButton rows={filters} onChange={onFiltersChange} disabled={disabled} />
        {fields.error !== null && (
          <Text size="caption" color="status-error">
            Could not load the fields of this event. Type the field paths, or close this and open it
            again to retry.
          </Text>
        )}
      </Stack>

      <ToggleRow
        label="Ignore its own runs"
        description="Skip events about runs this trigger started or messaged. Turn off only for a trigger meant to react to its own work."
        checked={ignoreOwnRuns}
        onChange={onIgnoreOwnRunsChange}
        disabled={disabled}
      />
    </Stack>
  );
}
