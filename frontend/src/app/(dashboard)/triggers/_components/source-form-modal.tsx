'use client';

/** @tier 3 — triggers-scoped. Create a webhook source and reveal its one-time
 * signing secret + inbound webhook path. */

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
  NativeSelect,
  Stack,
} from '@orbit/terra';
import type { WebhookSourceResponse } from '@orbit/shared/api';
import { createSource } from '../_services/triggers-api';

interface SourceFormModalProps {
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
}

export function SourceFormModal({ open, onClose, onSaved }: SourceFormModalProps) {
  const [name, setName] = useState('');
  const [provider, setProvider] = useState('github');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [created, setCreated] = useState<WebhookSourceResponse | null>(null);

  const handleSubmit = useCallback(async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setSubmitError('Name is required.');
      return;
    }
    setSubmitting(true);
    setSubmitError(null);
    const result = await createSource({ name: trimmed, provider });
    setSubmitting(false);
    if (result.error || !result.source) {
      setSubmitError(result.error ?? 'Failed to create source.');
      return;
    }
    setCreated(result.source);
  }, [name, provider]);

  const handleDone = useCallback(() => {
    setCreated(null);
    onSaved();
  }, [onSaved]);

  return (
    <Modal open={open} onClose={created ? handleDone : onClose} size="lg">
      {created ? (
        <>
          <ModalHeader
            title="Webhook source created"
            description="Copy the signing secret now — it is shown only once."
          />
          <ModalBody flushY>
            <Stack gap="comfortable">
              <Callout variant="warning">
                Store this secret securely. Configure it, along with the webhook path below, on the
                provider so deliveries can be verified.
              </Callout>
              <FormField id="source-secret" label="Signing secret">
                <Input id="source-secret" value={created.secret ?? ''} readOnly mono />
              </FormField>
              <FormField
                id="source-path"
                label="Webhook path"
                hint="Append this to your Orbit API base URL when configuring the provider."
              >
                <Input id="source-path" value={created.webhook_path} readOnly mono />
              </FormField>
            </Stack>
          </ModalBody>
          <ModalFooter>
            <Button size="sm" onClick={handleDone}>
              Done
            </Button>
          </ModalFooter>
        </>
      ) : (
        <>
          <ModalHeader
            title="New webhook source"
            description="Register an inbound origin (e.g. a GitHub repository) that can fire triggers."
          />
          <ModalBody flushY>
            <Stack gap="comfortable">
              <FormField id="source-name" label="Name">
                <Input
                  id="source-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. acme/widgets PRs"
                  maxLength={128}
                />
              </FormField>
              <FormField id="source-provider" label="Provider">
                <NativeSelect
                  id="source-provider"
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                >
                  <option value="github">GitHub</option>
                </NativeSelect>
              </FormField>
              {submitError && <Alert variant="error">{submitError}</Alert>}
            </Stack>
          </ModalBody>
          <ModalFooter>
            <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button
              size="sm"
              onClick={() => void handleSubmit()}
              disabled={submitting || !name.trim()}
            >
              {submitting ? 'Creating…' : 'Create source'}
            </Button>
          </ModalFooter>
        </>
      )}
    </Modal>
  );
}
