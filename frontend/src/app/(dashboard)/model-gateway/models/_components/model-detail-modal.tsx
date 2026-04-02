'use client';

import { useEffect, useRef, useCallback } from 'react';
import { Copy, X } from 'lucide-react';
import { Button, Heading, Text, Badge, Separator } from '@orbit/terra';
import type { ModelRow } from '../../_data/mock';
import { ProviderIcon } from './provider-icon';
import { AvailabilityDot } from './availability-dot';

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmtLatency(ms: number) {
  return ms === 0 ? '—' : `${ms.toLocaleString()} ms`;
}

function fmtContext(k: number) {
  if (k >= 1000) return `${(k / 1000).toFixed(0)}M`;
  return `${k}k`;
}

function fmtUsd(n: number, decimals = 2) {
  return `$${n.toFixed(decimals)}`;
}

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
              <ProviderIcon provider={model.provider} size="md" />
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
            {model.top && (
              <>
                <span className="inline-block h-4 w-px bg-border" aria-hidden />
                <Badge variant="default" size="sm">
                  {model.topLabel}
                </Badge>
              </>
            )}
          </div>

          {/* Data rows — all clickable to copy */}
          <div className="flex-1 overflow-y-auto">
            <div className="px-2 py-3">
              <div className="px-3 pb-1 uppercase tracking-wider">
                <Text size="caption" color="muted">
                  Identity
                </Text>
              </div>
              <CopyRow label="Model ID" value={model.id} mono />
              <CopyRow label="Provider" value={model.provider} />
              <CopyRow label="Model Name" value={model.name} />
            </div>

            <Separator />

            <div className="px-2 py-3">
              <div className="px-3 pb-1 uppercase tracking-wider">
                <Text size="caption" color="muted">
                  Performance
                </Text>
              </div>
              <CopyRow label="Latency" value={fmtLatency(model.latencyMs)} mono />
              <CopyRow label="Context Window" value={fmtContext(model.contextK)} mono />
            </div>

            <Separator />

            <div className="px-2 py-3">
              <div className="px-3 pb-1 uppercase tracking-wider">
                <Text size="caption" color="muted">
                  Pricing
                </Text>
              </div>
              <CopyRow label="Prompt ($/1M tokens)" value={fmtUsd(model.promptPer1M, 4)} mono />
              <CopyRow label="Output ($/1M tokens)" value={fmtUsd(model.outputPer1M, 4)} mono />
              <CopyRow label="Request Cost ($/req)" value={fmtUsd(model.requestCost, 6)} mono />
            </div>
          </div>

          {/* Footer */}
          <div className="border-t border-border px-5 py-3">
            <Text size="caption" color="subtle">
              Click any row above to copy the value to your clipboard.
            </Text>
          </div>
        </>
      )}
    </dialog>
  );
}
