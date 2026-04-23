'use client';

import { useCallback, useState } from 'react';

import {
  Button,
  Icon,
  Modal,
  ModalAccessibleTitle,
  ModalBody,
  ModalFooter,
  Stack,
  Text,
} from '@orbit/terra';
import type { CreatePATResponse } from '@orbit/shared/api';

// ── Types ─────────────────────────────────────────────────────────────────────

interface TokenCreatedModalProps {
  pat: CreatePATResponse | null;
  onClose: () => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function TokenCreatedModal({ pat, onClose }: TokenCreatedModalProps) {
  // Track which pat id was last copied so `copied` resets automatically when
  // a new token is shown — no setState-in-effect needed.
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const copied = copiedId === pat?.id;

  const handleCopy = useCallback(() => {
    if (!pat) return;
    void navigator.clipboard.writeText(pat.token).then(() => {
      setCopiedId(pat.id);
      setTimeout(() => setCopiedId(null), 2000);
    });
  }, [pat]);

  return (
    <Modal open={!!pat} onClose={onClose} size="lg" dismissOnOutsideClick={false}>
      {/* No ModalHeader here — we have a custom layout with a warning banner.
          ModalAccessibleTitle satisfies the Radix DialogTitle requirement. */}
      <ModalAccessibleTitle>API key created</ModalAccessibleTitle>

      {pat && (
        <>
          {/* Header */}
          <Stack asChild direction="horizontal" align="start" justify="between" gap="comfortable">
            <header className="border-b border-border p-5">
              <Stack gap="none">
                <Text size="body" weight="heavy" as="span">
                  API key created
                </Text>
                <Text size="supporting" color="muted">
                  {pat.name}
                </Text>
              </Stack>
            </header>
          </Stack>

          {/* Warning banner */}
          <Stack asChild direction="horizontal" align="start" gap="snug">
            <aside className="border-b border-border bg-status-degraded/10 px-5 py-3">
              <Icon name="triangle-alert" color="status-degraded" />
              <Stack gap="tight">
                <Text size="supporting" color="foreground" weight="heavy">
                  Copy this key now.
                </Text>
                <Text size="supporting" color="foreground">
                  It will not be shown again. If you lose it, you will need to revoke it and create
                  a new one.
                </Text>
              </Stack>
            </aside>
          </Stack>

          {/* Token */}
          <ModalBody>
            <Stack gap="tight">
              <Text size="caption" color="subtle" weight="emphasis">
                Your API key
              </Text>
              <button
                type="button"
                onClick={handleCopy}
                className="group flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-surface-elevated px-4 py-3 text-left transition-colors hover:bg-surface-overlay focus-visible:outline-2 focus-visible:outline-accent"
                aria-label="Copy API key to clipboard"
              >
                <Text size="supporting" mono truncate style={{ flex: 1, minWidth: 0 }}>
                  {pat.token}
                </Text>
                {copied ? (
                  <Icon name="check" color="status-healthy" />
                ) : (
                  <Icon name="copy" color="muted" />
                )}
              </button>
              <Text size="caption" color="subtle">
                {pat.expires_at
                  ? `Expires ${new Date(pat.expires_at).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })}`
                  : 'Never expires'}
              </Text>
            </Stack>
          </ModalBody>

          <ModalFooter>
            <Button variant="default" size="sm" onClick={onClose}>
              Done, I&apos;ve copied it
            </Button>
          </ModalFooter>
        </>
      )}
    </Modal>
  );
}
