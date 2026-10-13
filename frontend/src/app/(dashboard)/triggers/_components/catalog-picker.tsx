'use client';

/**
 * @tier 3 — triggers-scoped. A button opening Hades' searchable catalog list
 * for one event type or field. Shared by the wizard, drawer and filter rows.
 */

import { useState } from 'react';
import {
  Button,
  Icon,
  Popover,
  PopoverContent,
  PopoverTrigger,
  TemplateCatalogList,
  Text,
} from '@clear-route/hades';
import type { TemplateCatalogEntry } from '@clear-route/hades';

interface CatalogPickerProps {
  /** The current choice; the placeholder shows when empty. */
  value: string;
  placeholder: string;
  catalog: readonly TemplateCatalogEntry[];
  onSelect: (token: string, entry: TemplateCatalogEntry) => void;
  searchPlaceholder?: string;
  /** Lets `pattern` entries be chosen; the caller completes them. */
  selectablePatterns?: boolean;
  disabled?: boolean;
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
  'aria-label': ariaLabel,
}: CatalogPickerProps) {
  const [open, setOpen] = useState(false);
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
        <TemplateCatalogList
          catalog={catalog}
          searchPlaceholder={searchPlaceholder}
          selectablePatterns={selectablePatterns}
          onSelect={(token, entry) => {
            setOpen(false);
            onSelect(token, entry);
          }}
        />
      </PopoverContent>
    </Popover>
  );
}
