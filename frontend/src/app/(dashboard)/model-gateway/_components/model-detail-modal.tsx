'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { Check, Copy, X } from 'lucide-react';
import { Badge, Button, Heading, Stack, Text, Separator } from '@orbit/terra';
import type { ModelRow } from '../_types';
import { AvailabilityDot } from './availability-dot';

// ── CopyRow ───────────────────────────────────────────────────────────────────

interface CopyRowProps {
  label: string;
  value: string;
  mono?: boolean;
}

function CopyRow({ label, value, mono }: CopyRowProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, [value]);

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="group flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left transition-colors hover:bg-surface-elevated focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
    >
      <Stack gap="none">
        <Text size="caption" color="muted">
          {label}
        </Text>
        <Text size="supporting" weight="emphasis" mono={mono}>
          {value}
        </Text>
      </Stack>
      {copied ? (
        <Check className="ml-3 size-3.5 shrink-0 text-status-healthy" aria-hidden />
      ) : (
        <Copy
          className="ml-3 size-3.5 shrink-0 text-muted opacity-0 transition-opacity group-hover:opacity-100"
          aria-hidden
        />
      )}
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
    // eslint-disable-next-line local/no-raw-div
    <div className="flex flex-wrap items-center gap-2 px-2 py-1">
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

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (model) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [model]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handler = () => onClose();
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

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
      className="m-auto w-full max-w-lg rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm"
    >
      {model && (
        <>
          {/* Header */}
          {/* eslint-disable-next-line local/no-raw-div */}
          <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3">
            <Stack gap="none">
              <Heading variant="section">{model.name}</Heading>
              <Text size="caption" color="muted" mono>
                {model.id}
              </Text>
            </Stack>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
              <X className="size-4" />
            </Button>
          </div>

          {/* Status */}
          {/* eslint-disable-next-line local/no-raw-div */}
          <div className="flex items-center gap-2 border-b border-border px-4 py-2">
            <AvailabilityDot status={model.status} />
            <Text size="supporting">
              {model.status === 'available' ? 'Available' : 'Unavailable'}
            </Text>
          </div>

          {/* Scrollable body */}
          {/* eslint-disable-next-line local/no-raw-div */}
          <div className="max-h-[60vh] overflow-y-auto">
            {/* Identity */}
            {/* eslint-disable-next-line local/no-raw-div */}
            <div className="px-2 py-2">
              {/* eslint-disable-next-line local/no-raw-div */}
              <div className="px-2 pb-1">
                <Text size="caption" color="muted">
                  Identity
                </Text>
              </div>
              <CopyRow label="Model ID" value={model.id} mono />
              <CopyRow label="Provider" value={model.owned_by} />
              <CopyRow label="Display Name" value={model.name} />
              {model.aliases.length > 0 && <ChipRow label="Aliases" values={model.aliases} />}
            </div>

            <Separator />

            {/* Capabilities */}
            {/* eslint-disable-next-line local/no-raw-div */}
            <div className="px-2 py-2">
              {/* eslint-disable-next-line local/no-raw-div */}
              <div className="px-2 pb-1">
                <Text size="caption" color="muted">
                  Capabilities
                </Text>
              </div>
              <ChipRow label="Capabilities" values={model.capabilities} />
              <ChipRow label="API Dialects" values={model.dialects} />
            </div>

            <Separator />

            {/* Modalities */}
            {/* eslint-disable-next-line local/no-raw-div */}
            <div className="px-2 py-2">
              {/* eslint-disable-next-line local/no-raw-div */}
              <div className="px-2 pb-1">
                <Text size="caption" color="muted">
                  Modalities
                </Text>
              </div>
              <ChipRow label="Input" values={model.input_modalities} />
              <ChipRow label="Output" values={model.output_modalities} />
            </div>
          </div>

          {/* Footer */}
          {/* eslint-disable-next-line local/no-raw-div */}
          <div className="border-t border-border px-4 py-2">
            <Text size="caption" color="subtle">
              Click any identity row to copy to clipboard.
            </Text>
          </div>
        </>
      )}
    </dialog>
  );
}
