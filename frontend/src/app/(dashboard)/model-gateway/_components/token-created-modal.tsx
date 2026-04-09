'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Copy, TriangleAlert } from 'lucide-react';
import { Button, Heading, Stack, Text } from '@orbit/terra';
import type { CreatePATResponse } from '@orbit/shared/api';

// ── Types ─────────────────────────────────────────────────────────────────────

interface TokenCreatedModalProps {
  pat: CreatePATResponse | null;
  onClose: () => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function TokenCreatedModal({ pat, onClose }: TokenCreatedModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  // Track which pat id was last copied so `copied` resets automatically when
  // a new token is shown — no setState-in-effect needed.
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const copied = copiedId === pat?.id;

  // Sync dialog open/close with external <dialog> element (external system sync).
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (pat) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [pat]);

  // Sync close state back to parent (Escape key, etc.)
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handler = () => onClose();
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

  const handleCopy = useCallback(() => {
    if (!pat) return;
    void navigator.clipboard.writeText(pat.token).then(() => {
      setCopiedId(pat.id);
      setTimeout(() => setCopiedId(null), 2000);
    });
  }, [pat]);

  return (
    <dialog
      ref={dialogRef}
      className="m-auto w-full max-w-lg rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      {pat && (
        <>
          {/* Header */}
          <Stack asChild direction="horizontal" align="start" justify="between" gap="comfortable">
            <header className="border-b border-border p-5">
              <Stack gap="none">
                <Heading variant="section">API key created</Heading>
                <Text size="supporting" color="muted">
                  {pat.name}
                </Text>
              </Stack>
            </header>
          </Stack>

          {/* Warning banner */}
          <Stack asChild direction="horizontal" align="start" gap="snug">
            <aside className="border-b border-border bg-status-degraded/10 px-5 py-3">
              <TriangleAlert className="mt-0.5 size-4 shrink-0 text-status-degraded" aria-hidden />
              <Text size="supporting" color="foreground">
                <strong>Copy this key now.</strong> It will not be shown again. If you lose it, you
                will need to revoke it and create a new one.
              </Text>
            </aside>
          </Stack>

          {/* Token */}
          <section className="flex flex-col gap-2 p-5">
            <Text size="caption" color="subtle" weight="emphasis">
              Your API key
            </Text>
            <button
              type="button"
              onClick={handleCopy}
              className="group flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-surface-elevated px-4 py-3 text-left transition-colors hover:bg-surface-overlay focus-visible:outline-2 focus-visible:outline-accent"
              aria-label="Copy API key to clipboard"
            >
              <span className="flex-1 min-w-0">
                <Text size="supporting" mono truncate>
                  {pat.token}
                </Text>
              </span>
              {copied ? (
                <Check className="size-4 shrink-0 text-status-healthy" aria-hidden />
              ) : (
                <Copy className="size-4 shrink-0 text-muted" aria-hidden />
              )}
            </button>
            <Text size="caption" color="subtle">
              {pat.expires_at
                ? `Expires ${new Date(pat.expires_at).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })}`
                : 'Never expires'}
            </Text>
          </section>

          {/* Footer */}
          <Stack asChild direction="horizontal" justify="end">
            <footer className="border-t border-border px-5 py-3">
              <Button variant="default" size="sm" onClick={onClose}>
                Done, I&apos;ve copied it
              </Button>
            </footer>
          </Stack>
        </>
      )}
    </dialog>
  );
}
