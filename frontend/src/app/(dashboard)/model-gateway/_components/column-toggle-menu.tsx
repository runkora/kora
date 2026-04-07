'use client';

import { Settings2 } from 'lucide-react';
import { Button, Popover, PopoverTrigger, PopoverContent, Stack, Text } from '@orbit/terra';

// ── Types ─────────────────────────────────────────────────────────────────────

export type ColumnId =
  | 'favourite'
  | 'status'
  | 'provider'
  | 'name'
  | 'capabilities'
  | 'dialects'
  | 'inputModalities'
  | 'outputModalities';

export interface ColumnDef {
  id: ColumnId;
  label: string;
  locked?: boolean;
}

export const ALL_COLUMNS: ColumnDef[] = [
  { id: 'favourite', label: 'Favourite', locked: true },
  { id: 'status', label: 'Availability', locked: false },
  { id: 'provider', label: 'Provider', locked: false },
  { id: 'name', label: 'Model Name', locked: true },
  { id: 'capabilities', label: 'Capabilities', locked: false },
  { id: 'dialects', label: 'Dialects', locked: false },
  { id: 'inputModalities', label: 'Input Modalities', locked: false },
  { id: 'outputModalities', label: 'Output Modalities', locked: false },
];

interface ColumnToggleMenuProps {
  visible: Set<ColumnId>;
  onToggle: (id: ColumnId) => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ColumnToggleMenu({ visible, onToggle }: ColumnToggleMenuProps) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" aria-label="Toggle column visibility">
          <Settings2 className="size-3.5" />
          Columns
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <Stack asChild gap="none">
          <menu className="w-52 p-0 list-none m-0">
            <li className="border-b border-border px-3 py-2">
              <Text size="caption" color="muted">
                Toggle columns
              </Text>
            </li>
            <fieldset className="py-1 border-0 m-0 p-0">
              <legend className="sr-only">Visible columns</legend>
              {ALL_COLUMNS.map((col) => {
                const checked = visible.has(col.id);
                return (
                  <label
                    key={col.id}
                    className={[
                      'flex cursor-pointer items-center gap-2.5 px-3 py-2 text-sm transition-colors',
                      col.locked ? 'cursor-not-allowed opacity-50' : 'hover:bg-surface-elevated',
                    ].join(' ')}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={col.locked}
                      onChange={() => !col.locked && onToggle(col.id)}
                      className="accent-accent size-3.5 cursor-pointer rounded"
                    />
                    <Text size="supporting">{col.label}</Text>
                  </label>
                );
              })}
            </fieldset>
          </menu>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}
