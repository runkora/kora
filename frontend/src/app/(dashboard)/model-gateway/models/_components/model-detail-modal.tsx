'use client';

import { useEffect, useRef, useCallback } from 'react';
import { Copy, X } from 'lucide-react';
import { Badge, Button, Heading, Text, Separator } from '@orbit/terra';
import type { ModelRow } from '../../_types';
import { AvailabilityDot } from './availability-dot';

// ── CopyRow ───────────────────────────────────────────────────────────────────

interface CopyRowProps {
  label: string;
  value: string;
  mono?: boolean;
}

function CopyRow({ label, value, mono }: CopyRowProps) {
  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(value);
  }, [value]);

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="group flex w-full items-center justify-between rounded-md px-3 py-2.5 text-left transition-colors hover:bg-surface-elevated focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
    >
      <div className="flex flex-col gap-0.5">
        <Text size="caption" color="muted">
          {label}
        </Text>
        <Text size="supporting" weight="emphasis" mono={mono}>
          {value}
        </Text>
      </div>
      <Copy
        className="ml-3 size-3.5 shrink-0 text-muted opacity-0 transition-opacity group-hover:opacity-100"
        aria-hidden
      />
    </button>
  );
}

// ── ChipRow ───────────────────────────────────────────────────────────────────

interface ChipRowProps {
  label: string;
  values: string[];
}

function ChipRow({ label, values }: ChipRowProps) {
  return (
    <div className="flex flex-col gap-1.5 rounded-md px-3 py-2.5">
      <Text size="caption" color="muted">
        {label}
      </Text>
      {values.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {values.map((v) => (
            <Badge key={v} variant="outline" size="sm">
              {v}
            </Badge>
          ))}
        </div>
      ) : (
        <Text size="supporting" color="subtle">
          —
        </Text>
      )}
    </div>
  );
}

// ── SectionHeader ─────────────────────────────────────────────────────────────

function SectionHeader({ label }: { label: string }) {
  return (
    <div className="px-3 pb-1 pt-3 uppercase tracking-wider">
      <Text size="caption" color="muted">
        {label}
      </Text>
    </div>
  );
}

// ── ModelDetailModal ──────────────────────────────────────────────────────────

interface ModelDetailModalProps {
  model: ModelRow | null;
  onClose: () => void;
}

export function ModelDetailModal({ model, onClose }: ModelDetailModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  // Open / close the native dialog
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (model) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [model]);

  // Close on Escape (native dialog handles this, but we sync state)
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handler = () => onClose();
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

  // Close on backdrop click
  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDialogElement>) => {
      if (e.target === dialogRef.current) onClose();
    },
    [onClose],
  );

  return (
    <dialog
      ref={dialogRef}
      onClick={handleBackdropClick}
      className="m-auto w-full max-w-lg rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      {model && (
        <>
          {/* Header */}
          <div className="flex items-start justify-between gap-4 border-b border-border p-5">
            <div className="flex items-center gap-3">
              <div>
                <Heading variant="section">{model.name}</Heading>
                <Text size="caption" color="muted" mono>
                  {model.id}
                </Text>
              </div>
            </div>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
              <X className="size-4" />
            </Button>
          </div>

          {/* Status row */}
          <div className="flex items-center gap-3 border-b border-border px-5 py-3">
            <AvailabilityDot status={model.status} />
            <Text size="supporting">
              {model.status === 'available' ? 'Available' : 'Unavailable'}
            </Text>
          </div>

          {/* Data rows */}
          <div className="flex-1 overflow-y-auto">
            {/* Identity */}
            <div className="px-2 pb-2">
              <SectionHeader label="Identity" />
              <CopyRow label="Model ID" value={model.id} mono />
              <CopyRow label="Provider" value={model.owned_by} />
              <CopyRow label="Display Name" value={model.name} />
              {model.aliases.length > 0 && <ChipRow label="Aliases" values={model.aliases} />}
            </div>

            <Separator />

            {/* Capabilities */}
            <div className="px-2 pb-2">
              <SectionHeader label="Capabilities" />
              <ChipRow label="Capabilities" values={model.capabilities} />
              <ChipRow label="API Dialects" values={model.dialects} />
            </div>

            <Separator />

            {/* Modalities */}
            <div className="px-2 pb-2">
              <SectionHeader label="Modalities" />
              <ChipRow label="Input" values={model.input_modalities} />
              <ChipRow label="Output" values={model.output_modalities} />
            </div>
          </div>

          {/* Footer */}
          <div className="border-t border-border px-5 py-3">
            <Text size="caption" color="subtle">
              Click any identity row to copy the value to your clipboard.
            </Text>
          </div>
        </>
      )}
    </dialog>
  );
}
