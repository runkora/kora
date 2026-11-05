'use client';

/**
 * @tier 3 — triggers-scoped. The Event and Filters of a webhook or chat command
 * trigger, as one section: which event the trigger listens for, and which of its
 * deliveries start a run.
 *
 * The event applies to every filter below it, so it is its own block above them
 * rather than a row among them. Used by the New trigger wizard and by a stored
 * trigger's Configuration tab; the caller owns the state and what a change does
 * (the wizard clears the filters when the event changes, the modal does not).
 */

import { Card, SectionLabel, Stack, Text } from '@runkora/hades';
import { eventTypeCatalog, schemaCatalog } from './trigger-wizard/field-catalog';
import { AddFilterButton, FilterRowsEditor } from './filter-rows-editor';
import type { FilterRow } from './filter-rows-editor';
import { CatalogPicker } from './catalog-picker';
import { ChatEventPicker } from './chat-event-picker';
import { GENERIC_EVENT_TYPE } from '../_lib/generic-source';
import type { SourceCatalog } from './use-source-catalog';

interface EventFiltersSectionProps {
  /** A chat command offers its events as cards; any other webhook as a searchable list. */
  category: 'webhook' | 'chat';
  catalog: SourceCatalog;
  eventType: string;
  filters: FilterRow[];
  onEventTypeChange: (eventType: string) => void;
  onFiltersChange: (rows: FilterRow[]) => void;
  disabled?: boolean;
  /** A title above the surface, for a caller whose screen has no heading of its own. */
  title?: string;
  /** Help shown under the Event title. */
  eventHelp?: string;
  /** Help shown under the Filters title. */
  filtersHelp?: string;
  /** Marks filters that name a field but no value. */
  showIncomplete?: boolean;
  /** A source with no schema (a generic webhook): the event type and fields are typed, with the catalog as suggestions. */
  schemaless?: boolean;
}

export function EventFiltersSection({
  category,
  catalog,
  eventType,
  filters,
  onEventTypeChange,
  onFiltersChange,
  disabled = false,
  title,
  eventHelp,
  filtersHelp,
  showIncomplete = false,
  schemaless = false,
}: EventFiltersSectionProps) {
  const chat = category === 'chat';
  const surface = (
    <Card subtle border="none">
      <Stack gap="relaxed" padding="cozy">
        <Stack gap="snug">
          <Stack gap="tight">
            <SectionLabel>Event</SectionLabel>
            {eventHelp !== undefined && (
              <Text size="caption" color="subtle">
                {eventHelp}
              </Text>
            )}
          </Stack>
          {chat ? (
            <ChatEventPicker
              events={catalog.declared}
              value={eventType}
              disabled={disabled}
              onChange={onEventTypeChange}
            />
          ) : (
            <Stack direction="horizontal" gap="tight" align="center" paddingInline="snug">
              <Text as="span" size="supporting" color="muted">
                Listen for
              </Text>
              <CatalogPicker
                inline
                aria-label="Event type"
                value={eventType}
                placeholder={schemaless ? GENERIC_EVENT_TYPE : 'Choose an event'}
                catalog={eventTypeCatalog(catalog.declared, catalog.observed)}
                searchPlaceholder="Search events…"
                disabled={disabled}
                freeText={schemaless}
                onSelect={onEventTypeChange}
              />
            </Stack>
          )}
        </Stack>

        <Stack gap="snug">
          <Stack gap="tight">
            <SectionLabel>Filters</SectionLabel>
            {filtersHelp !== undefined && (
              <Text size="caption" color="subtle">
                {filtersHelp}
              </Text>
            )}
          </Stack>
          <FilterRowsEditor
            rows={filters}
            fields={schemaCatalog(catalog.fields)}
            onChange={onFiltersChange}
            disabled={disabled}
            showIncomplete={showIncomplete}
            freeText={schemaless}
            hideAdd
          />
          <AddFilterButton rows={filters} onChange={onFiltersChange} disabled={disabled} />
        </Stack>
      </Stack>
    </Card>
  );
  if (title === undefined) return surface;
  return (
    <Stack gap="snug">
      <SectionLabel>{title}</SectionLabel>
      {surface}
    </Stack>
  );
}
