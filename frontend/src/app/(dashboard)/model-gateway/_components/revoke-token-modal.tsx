'use client';

import { useCallback, useEffect, useRef } from 'react';
import { X } from 'lucide-react';
import { Button, Heading, Stack, Text } from '@orbit/terra';
import type { PAT } from '@orbit/shared/api';

// ── Types ─────────────────────────────────────────────────────────────────────

interface RevokeTokenModalProps {
  pat: PAT | null;
  revoking: boolean;
  error: string | null;
  onConfirm: (id: string) => void;
  onClose: () => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function RevokeTokenModal({
  pat,
  revoking,
  error,
  onConfirm,
  onClose,
}: RevokeTokenModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (pat) {
      if (!dialog.open) dialog.showModal();
    } else {
      if (dialog.open) dialog.close();
    }
  }, [pat]);

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
      className="m-auto w-full max-w-sm rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      {pat && (
        <>
          {/* Header */}
          <Stack asChild direction="horizontal" align="start" justify="between" gap="comfortable">
            <header className="border-b border-border p-5">
              <Stack gap="none">
                <Heading variant="section">Revoke API key</Heading>
                <Text size="supporting" color="muted">
                  {pat.name}
                </Text>
              </Stack>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={onClose}
                aria-label="Close"
                disabled={revoking}
              >
                <X className="size-4" />
              </Button>
            </header>
          </Stack>

          {/* Body */}
          {/* eslint-disable-next-line local/no-raw-div */}
          <div className="flex flex-col gap-3 p-5">
            <Text size="supporting" color="foreground">
              This key will be permanently revoked and any applications using it will lose access
              immediately. This cannot be undone.
            </Text>
            {error && (
              <Text size="supporting" color="status-error">
                {error}
              </Text>
            )}
          </div>

          {/* Footer */}
          <Stack asChild direction="horizontal" justify="end" gap="snug">
            <footer className="border-t border-border px-5 py-3">
              <Button variant="ghost" size="sm" onClick={onClose} disabled={revoking}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => onConfirm(pat.id)}
                disabled={revoking}
              >
                {revoking ? 'Revoking…' : 'Revoke key'}
              </Button>
            </footer>
          </Stack>
        </>
      )}
    </dialog>
  );
}
