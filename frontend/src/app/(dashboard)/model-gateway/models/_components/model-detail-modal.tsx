'use client';

import { useEffect, useRef, useCallback } from 'react';
import { Copy, X } from 'lucide-react';
import { Badge, Button, Heading, Stack, Text, Separator } from '@orbit/terra';
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
      <Stack gap="none">
        <Text size="caption" color="muted">
          {label}
        </Text>
        <Text size="supporting" weight="emphasis" mono={mono}>
          {value}
        </Text>
      </Stack>
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
    <Stack asChild gap="tight">
      <section className="rounded-md px-3 py-2.5">
        <Text size="caption" color="muted">
          {label}
        </Text>
        {values.length > 0 ? (
          <Stack direction="horizontal" wrap gap="tight">
            {values.map((v) => (
              <Badge key={v} variant="outline" size="sm">
                {v}
              </Badge>
            ))}
          </Stack>
        ) : (
          <Text size="supporting" color="subtle">
            —
          </Text>
        )}
      </section>
    </Stack>
  );
}

// ── SectionHeader ─────────────────────────────────────────────────────────────

function SectionHeader({ label }: { label: string }) {
  return (
    <Stack asChild>
      <header className="px-3 pb-1 pt-3 uppercase tracking-wider">
        <Text size="caption" color="muted">
          {label}
        </Text>
      </header>
    </Stack>
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
          <Stack asChild direction="horizontal" align="start" justify="between" gap="comfortable">
            <header className="border-b border-border p-5">
              <Stack direction="horizontal" align="center" gap="snug">
                <Stack gap="none">
                  <Heading variant="section">{model.name}</Heading>
                  <Text size="caption" color="muted" mono>
                    {model.id}
                  </Text>
                </Stack>
              </Stack>
              <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
                <X className="size-4" />
              </Button>
            </header>
          </Stack>

          {/* Status row */}
          <Stack asChild direction="horizontal" align="center" gap="comfortable">
            <section className="border-b border-border px-5 py-3">
              <AvailabilityDot status={model.status} />
              <Text size="supporting">
                {model.status === 'available' ? 'Available' : 'Unavailable'}
              </Text>
            </section>
          </Stack>

          {/* Data rows */}
          <Stack asChild>
            <article className="flex-1 overflow-y-auto">
              {/* Identity */}
              <Stack asChild>
                <section className="px-2 pb-2">
                  <SectionHeader label="Identity" />
                  <CopyRow label="Model ID" value={model.id} mono />
                  <CopyRow label="Provider" value={model.owned_by} />
                  <CopyRow label="Display Name" value={model.name} />
                  {model.aliases.length > 0 && <ChipRow label="Aliases" values={model.aliases} />}
                </section>
              </Stack>

              <Separator />

              {/* Capabilities */}
              <Stack asChild>
                <section className="px-2 pb-2">
                  <SectionHeader label="Capabilities" />
                  <ChipRow label="Capabilities" values={model.capabilities} />
                  <ChipRow label="API Dialects" values={model.dialects} />
                </section>
              </Stack>

              <Separator />

              {/* Modalities */}
              <Stack asChild>
                <section className="px-2 pb-2">
                  <SectionHeader label="Modalities" />
                  <ChipRow label="Input" values={model.input_modalities} />
                  <ChipRow label="Output" values={model.output_modalities} />
                </section>
              </Stack>
            </article>
          </Stack>

          {/* Footer */}
          <Stack asChild>
            <footer className="border-t border-border px-5 py-3">
              <Text size="caption" color="subtle">
                Click any identity row to copy the value to your clipboard.
              </Text>
            </footer>
          </Stack>
        </>
      )}
    </dialog>
  );
}
