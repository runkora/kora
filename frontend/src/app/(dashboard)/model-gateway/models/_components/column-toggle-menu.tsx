'use client';

import { useRef, useState, useEffect } from 'react';
import { Settings2 } from 'lucide-react';
import { Button, Text } from '@orbit/terra';

// ── Types ─────────────────────────────────────────────────────────────────────

export type ColumnId =
  | 'favourite'
  | 'status'
  | 'provider'
  | 'name'
  | 'latency'
  | 'context'
  | 'promptCost'
  | 'outputCost'
  | 'requestCost';

export interface ColumnDef {
  id: ColumnId;
  label: string;
  /** Columns that cannot be hidden */
  locked?: boolean;
}

export const ALL_COLUMNS: ColumnDef[] = [
  { id: 'favourite', label: 'Favourite', locked: true },
  { id: 'status', label: 'Availability', locked: false },
  { id: 'provider', label: 'Provider', locked: false },
  { id: 'name', label: 'Model Name', locked: true },
  { id: 'latency', label: 'Latency', locked: false },
  { id: 'context', label: 'Context', locked: false },
  { id: 'promptCost', label: 'Prompt ($/1M)', locked: false },
  { id: 'outputCost', label: 'Output ($/1M)', locked: false },
  { id: 'requestCost', label: 'Request Cost ($/Req)', locked: false },
];

interface ColumnToggleMenuProps {
  visible: Set<ColumnId>;
  onToggle: (id: ColumnId) => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ColumnToggleMenu({ visible, onToggle }: ColumnToggleMenuProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    function handler(e: MouseEvent) {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  // Close on Escape
  useEffect(() => {
    if (!open) return;
    function handler(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false);
    }
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [open]);

  return (
    <div ref={containerRef} className="relative">
      <Button
        variant="outline"
        size="sm"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-haspopup="listbox"
      >
        <Settings2 className="size-3.5" />
        Columns
      </Button>

      {open && (
        <div
          role="listbox"
          aria-multiselectable="true"
          aria-label="Toggle table columns"
          className="absolute right-0 top-full z-50 mt-1.5 w-52 rounded-lg border border-border bg-surface shadow-lg"
        >
          <div className="border-b border-border px-3 py-2">
            <Text size="caption" color="muted">
              Toggle columns
            </Text>
          </div>
          <div className="py-1">
            {ALL_COLUMNS.map((col) => {
              const checked = visible.has(col.id);
              return (
                <label
                  key={col.id}
                  role="option"
                  aria-selected={checked}
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
          </div>
        </div>
      )}
    </div>
  );
}
