/** @tier 1 — Zero kora domain knowledge. Could be extracted to a standalone library. */
import { Alert } from '../alert';
import { Button } from '../button';
import { Stack } from '../layout';
import { Modal, ModalBody, ModalFooter, ModalHeader } from '../modal';
import { Text } from '../typography';

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  confirming?: boolean;
  error?: string | null;
  /** Modal width. Defaults to `'sm'`. */
  size?: 'sm' | 'md' | 'lg';
  /** Extra body content rendered below the description (e.g. a summary card of what will be deleted). */
  children?: React.ReactNode;
}

/** Modal that asks the user to confirm an action that cannot be undone. */
export function ConfirmDialog({
  open,
  onClose,
  title,
  description,
  confirmLabel,
  onConfirm,
  confirming,
  error,
  size = 'sm',
  children,
}: ConfirmDialogProps) {
  return (
    <Modal open={open} onClose={onClose} size={size}>
      <ModalHeader title={title} />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <Stack gap="snug">
            <Text size="caption" color="muted">
              {description}
            </Text>
            {children}
          </Stack>
          {error && <Alert variant="error">{error}</Alert>}
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
