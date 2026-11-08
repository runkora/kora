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

/** The fields of one event type and their allowed values, from its schema. Empty until loaded. */
export function useEventTypeFields(eventType: string): SchemaCatalogEntry[] {
  const [answer, setAnswer] = useState<{ eventType: string; fields: SchemaCatalogEntry[] }>({
    eventType: '',
    fields: [],
  });
  useEffect(() => {
    if (eventType === '') return;
    let live = true;
    void fetchEventTypeFields(eventType).then((res) => {
      if (live) setAnswer({ eventType, fields: schemaCatalog(res.fields) });
    });
    return () => {
      live = false;
    };
  }, [eventType]);
  return answer.eventType === eventType ? answer.fields : [];
}

interface PlatformEventFieldsProps {
  eventType: string;
  /** The event type's fields, from `useEventTypeFields`. */
  fields: SchemaCatalogEntry[];
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
          fields={fields}
          onChange={onFiltersChange}
          freeText={fields.length === 0}
          hideAdd
          disabled={disabled}
        />
        <AddFilterButton rows={filters} onChange={onFiltersChange} disabled={disabled} />
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
