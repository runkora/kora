'use client';

/**
 * @tier 3 — triggers-scoped. The webhook match-condition builder.
 *
 * Used by the wizard (creating a trigger) and the modal (correcting one), so
 * the same gesture, vocabulary and placeholder appear on both surfaces. A row
 * is a field, an operator and its alternatives; every row must match for a
 * delivery to fire.
 */

import { useState } from 'react';
import { Card, Icon, InlineSelect, InlineToken, Input, Stack, Text } from '@runkora/hades';
import { CatalogPicker } from './catalog-picker';
import { completePattern } from './trigger-wizard/field-catalog';
import { splitFilterValues } from './trigger-wizard/wizard-state';
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
  /** Renders every row and Add filter disabled, for callers who may not change the filters. */
  disabled?: boolean;
  /** Fields are typed, for a source with no schema; `fields` are offered as suggestions. */
  freeText?: boolean;
  /** Leaves Add filter out, for a caller that places `AddFilterButton` itself. */
  hideAdd?: boolean;
  /** Marks rows that name a field but no value, as Save would refuse them. */
  showIncomplete?: boolean;
}

/** `where` and `and` padded to the same five monospaced characters, so rows line up. */
const LABEL_PAD_AND = 'and\u00a0\u00a0';
const LABEL_BLANK = '\u00a0'.repeat(5);

const OPERATORS = [
  { value: 'is', label: 'is' },
  { value: 'contains', label: 'contains' },
] as const;

/** Appends an empty row. Rendered by `FilterRowsEditor` unless `hideAdd` hands the placement over. */
export function AddFilterButton({
  rows,
  onChange,
  disabled = false,
}: Pick<FilterRowsEditorProps, 'rows' | 'onChange' | 'disabled'>) {
  return (
    <Stack direction="horizontal">
      {/* An InlineToken, not a ghost Button: the ghost hover fill is the same step as the card behind it. */}
      <InlineToken
        chevron={false}
        disabled={disabled}
        onClick={() => onChange([...rows, { field: '', values: '' }])}
      >
        <Stack direction="horizontal" gap="tight" align="center">
          <Icon name="plus" size="2xs" />
          Add filter
        </Stack>
      </InlineToken>
    </Stack>
  );
}

export function FilterRowsEditor({
  rows,
  fields,
  onChange,
  disabled = false,
  freeText = false,
  hideAdd = false,
  showIncomplete = false,
}: FilterRowsEditorProps) {
  // The row whose remove button is hovered or focused, so the whole filter reads as the thing removed.
  const [removing, setRemoving] = useState<number | null>(null);
  // The row the pointer is over or focus is inside; only that row offers its remove button.
  const [active, setActive] = useState<number | null>(null);
  const setRow = (index: number, next: Partial<FilterRow>) =>
    onChange(rows.map((row, i) => (i === index ? { ...row, ...next } : row)));

  return (
    <>
      {rows.map((row, i) => {
        const shown = active === i && !disabled;
        // Mirrors validateFilters: a row with a field but no values, or values but no field, is
        // half written and blocks Save. A row with neither is untouched, so it is only marked as a
        // place to type, and is dropped on save.
        const hasField = row.field.trim() !== '';
        const hasValues = splitFilterValues(row.values).length > 0;
        const valuesFill = hasValues ? undefined : hasField ? 'danger' : 'default';
        const fieldInvalid = !hasField && hasValues;
        const incomplete = showIncomplete && row.field !== '' && row.values.trim() === '';
        return (
          <Stack key={i} gap="tight">
            <Card
              subtle
              border="none"
              selected={removing === i}
              selectedTone="danger"
              onMouseEnter={() => setActive(i)}
              onMouseLeave={() => setActive(null)}
              onFocus={() => setActive(i)}
              onBlur={(e) => {
                if (!e.currentTarget.contains(e.relatedTarget)) setActive(null);
              }}
            >
              <Stack
                direction="horizontal"
                gap="tight"
                align="start"
                paddingInline="snug"
                paddingBlock="tight"
              >
                <Stack grow minWidth="none" gap="tight">
                  <Stack direction="horizontal" gap="tight" align="center">
                    <Text as="span" mono size="supporting" color="subtle">
                      {i === 0 ? 'where' : LABEL_PAD_AND}
                    </Text>
                    <Stack grow minWidth="none" direction="horizontal" gap="tight" align="center">
                      <CatalogPicker
                        inline
                        invalid={fieldInvalid}
                        aria-label={`Filter ${i + 1} field`}
                        value={row.pattern ?? row.field}
                        placeholder={freeText ? 'data.issue.title' : 'Choose a field'}
                        catalog={fields}
                        selectablePatterns
                        disabled={disabled}
                        freeText={freeText}
                        onSelect={(token, entry) =>
                          entry?.pattern
                            ? setRow(i, { pattern: token, patternName: '', field: '' })
                            : setRow(i, {
                                field: token,
                                pattern: undefined,
                                patternName: undefined,
                              })
                        }
                      />
                      {row.pattern !== undefined && (
                        <Input
                          mono
                          variant="inline"
                          filled={fieldInvalid ? 'danger' : undefined}
                          aria-label={`Filter ${i + 1} name`}
                          disabled={disabled}
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
                    </Stack>
                  </Stack>
                  <Stack direction="horizontal" gap="tight" align="center">
                    {/* Blank text as wide as the label above, so the operator sits under the field. */}
                    <Text as="span" mono size="supporting" aria-hidden>
                      {LABEL_BLANK}
                    </Text>
                    <InlineSelect
                      aria-label={`Filter ${i + 1} operator`}
                      disabled={disabled}
                      value={row.operator ?? 'is'}
                      options={OPERATORS}
                      onValueChange={(operator) =>
                        setRow(i, { operator: operator as FilterOperator })
                      }
                    />
                    <Stack grow minWidth="none">
                      <Input
                        mono
                        variant="inline"
                        tone="accent"
                        filled={valuesFill}
                        hasError={incomplete}
                        aria-label={`Filter ${i + 1} values`}
                        disabled={disabled}
                        value={row.values}
                        onChange={(e) => setRow(i, { values: e.target.value })}
                        placeholder={incomplete ? 'Add a value' : undefined}
                      />
                    </Stack>
                  </Stack>
                </Stack>
                {/* Always laid out, so the values field ends before it; only the row in use shows it. The padding centres it on the first line. */}
                <Stack paddingBlock="tight">
                  <InlineToken
                    tone="subtle"
                    chevron={false}
                    fill={false}
                    aria-label={`Remove filter ${i + 1}`}
                    aria-hidden={shown ? undefined : true}
                    disabled={disabled || !shown}
                    onMouseEnter={() => setRemoving(i)}
                    onMouseLeave={() => setRemoving(null)}
                    onFocus={() => setRemoving(i)}
                    onBlur={() => setRemoving(null)}
                    onClick={() => {
                      setRemoving(null);
                      onChange(rows.filter((_, x) => x !== i));
                    }}
                  >
                    <Icon name="close" size="xs" groupReveal={!shown} />
                  </InlineToken>
                </Stack>
              </Stack>
            </Card>
            {incomplete && (
              <Text size="caption" color="status-error">
                Choose at least one value, or remove this filter.
              </Text>
            )}
          </Stack>
        );
      })}

      {!hideAdd && <AddFilterButton rows={rows} onChange={onChange} disabled={disabled} />}
    </>
  );
}
