'use client';

/**
 * @tier 3 — triggers-scoped. A button opening Hades' searchable catalog list
 * for one event type or field. Shared by the wizard, drawer and filter rows.
 * With `freeText`, a source without a schema: the value is typed, and the
 * catalog is offered as suggestions.
 */

import { useState } from 'react';
import {
  Button,
  Icon,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Stack,
  TemplateCatalogList,
  Text,
} from '@runkora/hades';
import type { TemplateCatalogEntry } from '@runkora/hades';

interface CatalogPickerProps {
  /** The current choice; the placeholder shows when empty. */
  value: string;
  placeholder: string;
  catalog: readonly TemplateCatalogEntry[];
  /** `entry` is absent for a typed value. */
  onSelect: (token: string, entry?: TemplateCatalogEntry) => void;
  searchPlaceholder?: string;
  /** Lets `pattern` entries be chosen; the caller completes them. */
  selectablePatterns?: boolean;
  disabled?: boolean;
  /** Takes a typed value; the catalog becomes suggestions. */
  freeText?: boolean;
  'aria-label': string;
}

export function CatalogPicker({
  value,
  placeholder,
  catalog,
  onSelect,
  searchPlaceholder,
  selectablePatterns,
  disabled,
  freeText = false,
  'aria-label': ariaLabel,
}: CatalogPickerProps) {
  const [open, setOpen] = useState(false);
  const list = (
    <TemplateCatalogList
      catalog={catalog}
      searchPlaceholder={searchPlaceholder}
      selectablePatterns={selectablePatterns}
      onSelect={(token, entry) => {
        setOpen(false);
        onSelect(token, entry);
      }}
    />
  );

  if (freeText) {
    return (
      <Stack direction="horizontal" gap="snug" align="center">
        <Stack grow minWidth="none">
          <Input
            mono
            aria-label={ariaLabel}
            value={value}
            disabled={disabled}
            placeholder={placeholder}
            onChange={(e) => onSelect(e.target.value)}
          />
        </Stack>
        {catalog.length > 0 && (
          <Popover modal open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
              <Button
                type="button"
                variant="outline-muted"
                size="icon-sm"
                disabled={disabled}
                aria-label={`Suggestions for ${ariaLabel.toLowerCase()}`}
              >
                <Icon name="chevron-down" size="2xs" />
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" matchTriggerWidth>
              {list}
            </PopoverContent>
          </Popover>
        )}
      </Stack>
    );
  }

  return (
    <Popover modal open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline-muted"
          size="sm"
          fullWidth
          disabled={disabled}
          aria-label={ariaLabel}
          title={value || undefined}
        >
          <Text
            as="span"
            mono
            size="caption"
            truncate="start"
            color={value ? 'foreground' : 'subtle'}
          >
            {value || placeholder}
          </Text>
          <Icon name="chevron-down" size="2xs" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" matchTriggerWidth>
        {list}
      </PopoverContent>
    </Popover>
  );
}
