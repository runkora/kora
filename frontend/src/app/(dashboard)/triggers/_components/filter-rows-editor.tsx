'use client';

/**
 * @tier 3 — triggers-scoped. The webhook match-condition builder.
 *
 * Used by the wizard (creating a trigger) and the drawer (correcting one), so
 * the same gesture, vocabulary and placeholder appear on both surfaces. A row
 * is a field and its alternatives; every row must match for a delivery to fire.
 */

import { Button, Icon, Input, NativeSelect, Stack } from '@orbit/terra';

/**
 * One editable row. `values` stays a raw string while being typed: splitting on
 * every keystroke would fight the person entering the second alternative.
 */
export interface FilterRow {
  field: string;
  values: string;
}

interface FilterRowsEditorProps {
  rows: FilterRow[];
  /** The fields this event carries, offered in the picker. */
  fields: { token: string }[];
  onChange: (next: FilterRow[]) => void;
  /** Offer the Add filter button. Read-only callers pass false. */
  canAdd?: boolean;
}

export function FilterRowsEditor({ rows, fields, onChange, canAdd = true }: FilterRowsEditorProps) {
  const setRow = (index: number, next: Partial<FilterRow>) =>
    onChange(rows.map((row, i) => (i === index ? { ...row, ...next } : row)));

  return (
    <>
      {rows.map((row, i) => (
        <Stack key={i} direction="horizontal" gap="snug" align="center">
          <Stack minWidth="form-field-sm">
            <NativeSelect
              mono
              aria-label={`Filter ${i + 1} field`}
              value={row.field}
              onChange={(e) => setRow(i, { field: e.target.value })}
            >
              <option value="">Choose a field</option>
              {fields.map((entry) => (
                <option key={entry.token} value={entry.token}>
                  {entry.token}
                </option>
              ))}
              {/* A stored filter may name a field outside the current event's
                  catalog; keep it selectable so editing the row cannot
                  silently drop it. */}
              {row.field !== '' && !fields.some((entry) => entry.token === row.field) && (
                <option value={row.field}>{row.field}</option>
              )}
            </NativeSelect>
          </Stack>
          <Stack grow minWidth="none">
            <Input
              mono
              aria-label={`Filter ${i + 1} values`}
              value={row.values}
              onChange={(e) => setRow(i, { values: e.target.value })}
              placeholder="opened | synchronize"
            />
          </Stack>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Remove filter ${i + 1}`}
            onClick={() => onChange(rows.filter((_, x) => x !== i))}
          >
            <Icon name="close" size="2xs" />
          </Button>
        </Stack>
      ))}

      {canAdd && (
        <Stack direction="horizontal">
          <Button
            variant="ghost"
            size="xs"
            onClick={() => onChange([...rows, { field: '', values: '' }])}
          >
            <Icon name="plus" size="2xs" />
            Add filter
          </Button>
        </Stack>
      )}
    </>
  );
}
