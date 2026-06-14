'use client';

/** @tier 3 — environments-scoped. Create/edit modal for an environment: name,
 * description, egress-hosts editor, and the shared connectors picker. */

import { useCallback, useState } from 'react';
import {
  Alert,
  Button,
  FormField,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Text,
  Textarea,
} from '@orbit/terra';
import type { ConnectionCatalogEntry, Connector } from '@orbit/shared/api';
import { ConnectorsFields } from '../../agents/_components/connectors-fields';
import { createEnvironment, updateEnvironment } from '../_services/environments-api';
import { EgressHostsEditor } from './egress-hosts-editor';
import type { EnvironmentRow } from '../_types';

interface EnvironmentFormModalProps {
  open: boolean;
  /** When set, the modal edits this environment; otherwise it creates a new one. */
  environment: EnvironmentRow | null;
  connections: ConnectionCatalogEntry[];
  onClose: () => void;
  onSaved: () => void;
}

export function EnvironmentFormModal({
  open,
  environment,
  connections,
  onClose,
  onSaved,
}: EnvironmentFormModalProps) {
  const isEdit = environment !== null;

  const [name, setName] = useState(environment?.name ?? '');
  const [description, setDescription] = useState(environment?.description ?? '');
  const [egressHosts, setEgressHosts] = useState<string[]>(environment?.egressHosts ?? []);
  const [connectors, setConnectors] = useState<Connector[]>(environment?.connectors ?? []);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setSubmitError('Name is required.');
      return;
    }
    setSubmitting(true);
    setSubmitError(null);
    const payload = {
      name: trimmedName,
      description: description.trim(),
      egress_hosts: egressHosts,
      connectors,
    };
    const result = isEdit
      ? await updateEnvironment(environment.id, payload)
      : await createEnvironment(payload);
    setSubmitting(false);
    if (result.error) {
      setSubmitError(result.error);
      return;
    }
    onSaved();
  }, [name, description, egressHosts, connectors, isEdit, environment, onSaved]);

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader
        title={isEdit ? 'Edit environment' : 'New environment'}
        description="Bundle the egress hosts and connectors an agent type can reference."
      />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <FormField id="environment-name" label="Name">
            <Input
              id="environment-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. dev-registries"
              maxLength={128}
            />
          </FormField>

          <FormField
            id="environment-description"
            label="Description"
            hint="Optional summary of what this environment grants."
          >
            <Textarea
              id="environment-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="e.g. Public package registries needed to install dependencies."
              rows={2}
            />
          </FormField>

          <Stack gap="snug">
            <Text size="caption" weight="emphasis">
              Egress hosts
            </Text>
            <EgressHostsEditor value={egressHosts} onChange={setEgressHosts} />
          </Stack>

          <Stack gap="snug">
            <Text size="caption" weight="emphasis">
              Connectors
            </Text>
            <ConnectorsFields available={connections} value={connectors} onChange={setConnectors} />
          </Stack>

          {submitError && <Alert variant="error">{submitError}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => void handleSubmit()} disabled={submitting || !name.trim()}>
          {submitting
            ? isEdit
              ? 'Saving…'
              : 'Creating…'
            : isEdit
              ? 'Save changes'
              : 'Create environment'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
