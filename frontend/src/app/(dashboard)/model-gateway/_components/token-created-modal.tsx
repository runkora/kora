'use client';

import {
  CopyField,
  Alert,
  Icon,
  Modal,
  ModalAccessibleTitle,
  ModalBody,
  ModalFooter,
  Stack,
  Text,
  Button,
} from '@orbit/hades';
import type { CreatePATResponse } from '@orbit/shared/api';

// ── Types ─────────────────────────────────────────────────────────────────────

interface TokenCreatedModalProps {
  pat: CreatePATResponse | null;
  onClose: () => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function TokenCreatedModal({ pat, onClose }: TokenCreatedModalProps) {
  return (
    <Modal open={!!pat} onClose={onClose} size="lg" dismissOnOutsideClick={false}>
      {/* No ModalHeader here — we have a custom layout with a warning banner.
          ModalAccessibleTitle satisfies the Radix DialogTitle requirement. */}
      <ModalAccessibleTitle>API key created</ModalAccessibleTitle>

      {pat && (
        <>
          {/* Header */}
          <Stack
            direction="horizontal"
            align="start"
            justify="between"
            gap="comfortable"
            paddingBlock="comfortable"
            paddingInline="comfortable"
          >
            <Stack gap="none">
              <Text size="body" weight="heavy" as="span">
                API key created
              </Text>
              <Text size="supporting" color="muted">
                {pat.name}
              </Text>
            </Stack>
          </Stack>

          {/* Body: warning banner + token */}
          <ModalBody>
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
            <Stack gap="tight">
              <Text size="caption" color="subtle" weight="emphasis">
                Your API key
              </Text>
              <CopyField
                key={pat.token}
                value={pat.token}
                ariaLabel="Copy API key to clipboard"
                mono
              />
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
