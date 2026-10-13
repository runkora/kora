'use client';

/**
 * @tier 3 — triggers-scoped. The webhook match-condition builder.
 *
 * Used by the wizard (creating a trigger) and the drawer (correcting one), so
 * the same gesture, vocabulary and placeholder appear on both surfaces. A row
 * is a field, an operator and its alternatives; every row must match for a
 * delivery to fire.
 */

import { Button, Icon, Input, NativeSelect, Separator, Stack } from '@runkora/hades';
import { CatalogPicker } from './catalog-picker';
import { completePattern } from './trigger-wizard/field-catalog';
import type { SchemaCatalogEntry } from './trigger-wizard/field-catalog';

/**
 * One editable row. `values` stays a raw string while being typed: splitting on
 * every keystroke would fight the person entering the second alternative.
 */
export interface FilterRow {
  field: string;
  /** `is` matches a value exactly; `contains` matches text in it, ignoring case. Absent means `is`. */
  operator?: FilterOperator;
  values: string;
  /**
   * The pattern family being completed, e.g. `commonLabels.<name>`, while the
   * author types the name. `field` holds the completed path.
   */
  pattern?: string;
  /** The name typed to complete `pattern`. */
  patternName?: string;
}

export type FilterOperator = 'is' | 'contains';

interface FilterRowsEditorProps {
  rows: FilterRow[];
  /** The fields this event carries, offered in the picker. */
  fields: SchemaCatalogEntry[];
  onChange: (next: FilterRow[]) => void;
  /** Offer the Add filter button. Read-only callers pass false. */
  canAdd?: boolean;
}

const DEFAULT_VALUES_HINT = 'value | other';
const CONTAINS_HINT = 'text | other';

export function FilterRowsEditor({ rows, fields, onChange, canAdd = true }: FilterRowsEditorProps) {
  const setRow = (index: number, next: Partial<FilterRow>) =>
    onChange(rows.map((row, i) => (i === index ? { ...row, ...next } : row)));

  const valuesHint = (row: FilterRow) => {
    if (row.operator === 'contains') return CONTAINS_HINT;
    const allowed = fields.find((entry) => entry.token === row.field)?.enum;
    return allowed ? allowed.join(' | ') : DEFAULT_VALUES_HINT;
  };

  return (
    <>
      {rows.map((row, i) => (
        <Stack key={i} gap="snug">
          {i > 0 && <Separator />}
          <Stack direction="horizontal" gap="snug" align="center">
            <Stack grow minWidth="none">
              <CatalogPicker
                aria-label={`Filter ${i + 1} field`}
                value={row.pattern ?? row.field}
                placeholder="Choose a field"
                catalog={fields}
                selectablePatterns
                onSelect={(token, entry) =>
                  entry.pattern
                    ? setRow(i, { pattern: token, patternName: '', field: '' })
                    : setRow(i, { field: token, pattern: undefined, patternName: undefined })
                }
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
          {row.pattern !== undefined && (
            <Input
              mono
              aria-label={`Filter ${i + 1} name`}
              value={row.patternName ?? ''}
              onChange={(e) =>
                setRow(i, {
                  patternName: e.target.value,
                  field: completePattern(row.pattern ?? '', e.target.value),
                })
              }
              placeholder="name"
            />
          )}
          <Stack direction="horizontal" gap="snug" align="center">
            <Stack maxWidth="form-field-sm">
              <NativeSelect
                aria-label={`Filter ${i + 1} operator`}
                value={row.operator ?? 'is'}
                onChange={(e) => setRow(i, { operator: e.target.value as FilterOperator })}
              >
                <option value="is">is</option>
                <option value="contains">contains</option>
              </NativeSelect>
            </Stack>
            <Stack grow minWidth="none">
              <Input
                mono
                aria-label={`Filter ${i + 1} values`}
                value={row.values}
                onChange={(e) => setRow(i, { values: e.target.value })}
                placeholder={valuesHint(row)}
              />
            </Stack>
          </Stack>
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
