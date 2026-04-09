'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { Button, Heading, Stack, Text } from '@orbit/terra';
import { createToken } from '../_actions/tokens';
import type { CreatePATResponse } from '@orbit/shared/api';

// ── Types ─────────────────────────────────────────────────────────────────────

interface CreateTokenModalProps {
  open: boolean;
  onClose: () => void;
  onCreated: (pat: CreatePATResponse) => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function CreateTokenModal({ open, onClose, onCreated }: CreateTokenModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const nameInputRef = useRef<HTMLInputElement>(null);

  const [name, setName] = useState('');
  const [expiresInDays, setExpiresInDays] = useState<string>('90');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Sync dialog open/close with the external <dialog> element.
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open) {
      if (!dialog.open) dialog.showModal();
      setTimeout(() => nameInputRef.current?.focus(), 0);
    } else {
      if (dialog.open) dialog.close();
    }
  }, [open]);

  // Reset form state and notify parent when the dialog closes (Escape, backdrop,
  // or programmatic close). setState is called inside the event callback — not
  // synchronously in the effect body — so it does not cause cascading renders.
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handler = () => {
      setName('');
      setExpiresInDays('90');
      setError(null);
      onClose();
    };
    dialog.addEventListener('close', handler);
    return () => dialog.removeEventListener('close', handler);
  }, [onClose]);

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent<HTMLDialogElement>) => {
      if (e.target === dialogRef.current) onClose();
    },
    [onClose],
  );

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setError(null);

      const trimmedName = name.trim();
      if (!trimmedName) {
        setError('Name is required.');
        return;
      }
      if (trimmedName.length > 128) {
        setError('Name must be 128 characters or fewer.');
        return;
      }

      const days = expiresInDays === '' ? undefined : Number(expiresInDays);
      if (days !== undefined && (isNaN(days) || days < 1 || days > 365)) {
        setError('Expiry must be between 1 and 365 days.');
        return;
      }

      setSubmitting(true);
      const result = await createToken({ name: trimmedName, expires_in_days: days });
      setSubmitting(false);

      if (!result.ok) {
        setError(result.message);
        return;
      }

      onCreated(result.pat);
    },
    [name, expiresInDays, onCreated],
  );

  return (
    <dialog
      ref={dialogRef}
      onClick={handleBackdropClick}
      className="m-auto w-full max-w-md rounded-xl border border-border bg-surface p-0 shadow-2xl backdrop:bg-black/50 backdrop:backdrop-blur-sm open:flex open:flex-col"
    >
      <form onSubmit={(e) => void handleSubmit(e)} noValidate>
        {/* Header */}
        <Stack asChild direction="horizontal" align="start" justify="between" gap="comfortable">
          <header className="border-b border-border p-5">
            <Stack gap="none">
              <Heading variant="section">Create API key</Heading>
              <Text size="supporting" color="muted">
                Keys are scoped to your account and usable at inference endpoints.
              </Text>
            </Stack>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={onClose}
              aria-label="Close"
            >
              <X className="size-4" />
            </Button>
          </header>
        </Stack>

        {/* Body — native section used as padding wrapper so className stays off Terra Stack */}
        <section className="flex flex-col gap-5 p-5">
          {/* Name */}
          <Stack gap="tight">
            <label htmlFor="pat-name" className="text-sm font-medium text-foreground">
              Name <span aria-hidden>*</span>
            </label>
            <input
              ref={nameInputRef}
              id="pat-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Production Agent Fleet"
              maxLength={128}
              required
              className="w-full rounded-md border border-border bg-surface-elevated px-3 py-2 text-sm text-foreground placeholder:text-muted focus:outline-none focus:ring-2 focus:ring-accent"
            />
            <Text size="caption" color="subtle">
              A short label to identify this key. Max 128 characters.
            </Text>
          </Stack>

          {/* Expiry */}
          <Stack gap="tight">
            <label htmlFor="pat-expiry" className="text-sm font-medium text-foreground">
              Expires in (days)
            </label>
            <input
              id="pat-expiry"
              type="number"
              value={expiresInDays}
              onChange={(e) => setExpiresInDays(e.target.value)}
              min={1}
              max={365}
              placeholder="90"
              className="w-full rounded-md border border-border bg-surface-elevated px-3 py-2 text-sm text-foreground placeholder:text-muted focus:outline-none focus:ring-2 focus:ring-accent"
            />
            <Text size="caption" color="subtle">
              Defaults to 90 days. Maximum 365 days.
            </Text>
          </Stack>

          {/* Error */}
          {error && (
            <Text size="supporting" color="status-error">
              {error}
            </Text>
          )}
        </section>

        {/* Footer */}
        <Stack asChild direction="horizontal" justify="end" gap="snug">
          <footer className="border-t border-border px-5 py-3">
            <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" variant="default" size="sm" disabled={submitting}>
              {submitting ? 'Creating…' : 'Create key'}
            </Button>
          </footer>
        </Stack>
      </form>
    </dialog>
  );
}
