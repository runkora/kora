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
  // Defaults to "main" — the CRD's own symbolic default, which resolves to
  // whatever version the host Orbit platform is itself currently running.
  // Clear it and type an immutable tag (e.g. v1.2.3) to pin a different
  // build instead.
  const [ref, setRef] = useState('main');
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
    setRef('main');
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
            label="Version"
            hint={
              '"main" matches the version Orbit is currently running, or pin an immutable image tag (e.g. v1.2.3)'
            }
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
