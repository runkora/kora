'use client';

/** @tier 3 — dev-environments-scoped. Provision a new ephemeral dev environment. */

import { useCallback, useState } from 'react';
import {
  Alert,
  Button,
  Callout,
  FormField,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
} from '@orbit/terra';

interface ProvisionModalProps {
  open: boolean;
  onClose: () => void;
  onCreate: (ref: string) => Promise<{ error: string | null }>;
}

export function ProvisionModal({ open, onClose, onCreate }: ProvisionModalProps) {
  const [ref, setRef] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    setSubmitting(true);
    setSubmitError(null);
    const result = await onCreate(ref.trim());
    setSubmitting(false);
    if (result.error) {
      setSubmitError(result.error);
      return;
    }
    setRef('');
    onClose();
  }, [ref, onCreate, onClose]);

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader
        title="New dev environment"
        description="Provisions a disposable, full-stack Orbit install as a vcluster — real operator, real charts, real SPIRE-issued identities."
      />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <FormField
            id="devenv-ref"
            label="Environment name"
            hint="Name the ephemeral environment to create"
          >
            <Input
              id="devenv-ref"
              value={ref}
              onChange={(e) => setRef(e.target.value)}
              placeholder="main"
              maxLength={256}
              mono
            />
          </FormField>
          <Callout variant="info">
            Ready in a few minutes. Default TTL is 24 hours, extendable up to 7 days; idle
            environments sleep after 2 hours and can be woken from this page.
          </Callout>
          {submitError && <Alert variant="error">{submitError}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => void handleSubmit()} disabled={submitting}>
          {submitting ? 'Provisioning…' : 'Provision environment'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
