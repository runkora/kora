'use client';

import { useCallback, useState } from 'react';

import {
  Alert,
  Button,
  Icon,
  Modal,
  ModalAccessibleTitle,
  ModalBody,
  ModalFooter,
  Separator,
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
          <Stack direction="horizontal" align="start" justify="between" gap="comfortable" paddingBlock="comfortable" paddingInline="comfortable">
              <Stack gap="none">
                <Text size="body" weight="heavy" as="span">
                  API key created
                </Text>
                <Text size="supporting" color="muted">
                  {pat.name}
                </Text>
              </Stack>
          </Stack>
          <Separator />

          {/* Warning banner */}
          <Alert variant="warning">
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
          </Alert>

          {/* Token */}
          <ModalBody>
            <Stack gap="tight">
              <Text size="caption" color="subtle" weight="emphasis">
                Your API key
              </Text>
              <Button
                variant="secondary"
                fullWidth
                onClick={handleCopy}
                aria-label="Copy API key to clipboard"
              >
                <Stack direction="horizontal" align="center" justify="between" grow>
                  <Stack grow minWidth="none">
                    <Text size="supporting" mono truncate>
                      {pat.token}
                    </Text>
                  </Stack>
                  {copied ? (
                    <Icon name="check" color="status-healthy" />
                  ) : (
                    <Icon name="copy" color="muted" />
                  )}
                </Stack>
              </Button>
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
