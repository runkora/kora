'use client';

/** @tier 3 — dev-environments-scoped. Raise an environment's TTL, up to the
 * operator's 7-day hard cap (decision 8). */

import { useCallback, useState } from 'react';
import {
  Alert,
  Button,
  FormField,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  NativeSelect,
  Stack,
  Text,
} from '@orbit/terra';
import type { DevEnvironment } from '@orbit/shared/api';
import { formatTimestamp, TTL_PRESETS } from '../_types';

interface ExtendModalProps {
  environment: DevEnvironment | null;
  onClose: () => void;
  onExtend: (id: string, ttl: string) => Promise<{ error: string | null }>;
}

export function ExtendModal({ environment, onClose, onExtend }: ExtendModalProps) {
  const [hours, setHours] = useState(TTL_PRESETS[1]?.hours ?? 24);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    if (!environment) return;
    setSubmitting(true);
    setSubmitError(null);
    const result = await onExtend(environment.id, `${hours}h`);
    setSubmitting(false);
    if (result.error) {
      setSubmitError(result.error);
      return;
    }
    onClose();
  }, [environment, hours, onExtend, onClose]);

  return (
    <Modal open={!!environment} onClose={onClose} size="lg">
      <ModalHeader
        title="Extend dev environment"
        description="Sets the total lifetime from creation — not additional time on top of what's left."
      />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <FormField id="devenv-ttl" label="New total TTL">
            <NativeSelect
              id="devenv-ttl"
              value={String(hours)}
              onChange={(e) => setHours(Number(e.target.value))}
            >
              {TTL_PRESETS.map((preset) => (
                <option key={preset.hours} value={preset.hours}>
                  {preset.label}
                </option>
              ))}
            </NativeSelect>
          </FormField>
          <Text size="caption" color="subtle">
            Current expiry: {formatTimestamp(environment?.expiresAt)}
          </Text>
          {submitError && <Alert variant="error">{submitError}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => void handleSubmit()} disabled={submitting}>
          {submitting ? 'Extending…' : 'Extend'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
