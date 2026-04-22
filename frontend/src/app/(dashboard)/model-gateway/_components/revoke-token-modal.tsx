'use client';

import { Button, Modal, ModalBody, ModalFooter, ModalHeader, Text } from '@orbit/terra';
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
  return (
    <Modal open={!!pat} onClose={onClose} size="sm">
      <ModalHeader title="Revoke API key" description={pat?.name ?? ''} />
      <ModalBody>
        <Text size="supporting" color="foreground">
          This key will be permanently revoked and any applications using it will lose access
          immediately. This cannot be undone.
        </Text>
        {error && (
          <Text size="supporting" color="status-error">
            {error}
          </Text>
        )}
      </ModalBody>
      <ModalFooter>
        <Button variant="ghost" size="sm" onClick={onClose} disabled={revoking}>
          Cancel
        </Button>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => pat && onConfirm(pat.id)}
          disabled={revoking}
        >
          {revoking ? 'Revoking…' : 'Revoke key'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
