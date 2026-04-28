/**
 * @tier 2 — App-level shared. Cross-feature; encodes the Orbit destructive-action
 * confirmation pattern: warning text, optional inline body content, cancel + destructive button.
 * Used by policy (grants, roles), model-gateway (tokens), and any future destructive flow.
 * @promote-to-tier-1 if: destructive-action semantics and error display are removed and it
 * becomes a generic confirmation dialog.
 */
import { Button, Modal, ModalBody, ModalFooter, ModalHeader, Stack, Text } from '@orbit/terra';

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  confirming?: boolean;
  error?: string | null;
  /** Extra body content rendered below the description (e.g. a summary card of what will be deleted). */
  children?: React.ReactNode;
}

export function ConfirmDialog({
  open,
  onClose,
  title,
  description,
  confirmLabel,
  onConfirm,
  confirming,
  error,
  children,
}: ConfirmDialogProps) {
  return (
    <Modal open={open} onClose={onClose} size="sm">
      <ModalHeader title={title} />
      <ModalBody>
        <Stack gap="comfortable">
          <Stack gap="snug">
            <Text size="caption" color="muted">
              {description}
            </Text>
            {children}
          </Stack>
          {error && (
            <Text color="status-error" size="caption">
              {error}
            </Text>
          )}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={confirming}>
          Cancel
        </Button>
        <Button variant="destructive" size="sm" onClick={onConfirm} disabled={confirming}>
          {confirming ? 'Working…' : confirmLabel}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
