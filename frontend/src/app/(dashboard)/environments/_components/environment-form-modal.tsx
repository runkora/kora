'use client';

/** @tier 3 — environments-scoped. Create/edit modal for an environment: name,
 * description, egress-hosts editor, the shared connection cards, and a live
 * effective-access preview. */

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Eyebrow,
  FormField,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Textarea,
} from '@orbit/terra';
import type { ConnectionCatalogEntry, Connector, EffectiveAccess } from '@orbit/shared/api';
import { ConnectionCards, EffectiveAccessPanel } from '@orbit/components';
import {
  createEnvironment,
  previewEnvironmentAccess,
  updateEnvironment,
} from '../_services/environments-api';
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

  // ── Effective access preview ────────────────────────────────────────────────
  // An environment is exactly a posture + connectors bundle, so the same
  // resolver behind the agent surfaces answers for a draft environment.
  // Debounced so typing in the hosts editor doesn't spam the endpoint.
  const [access, setAccess] = useState<EffectiveAccess | null>(null);
  const [accessError, setAccessError] = useState<string | null>(null);
  useEffect(() => {
    const timer = setTimeout(() => {
      void previewEnvironmentAccess({ egressHosts, connectors }).then((result) => {
        setAccess(result.access);
        setAccessError(result.error);
      });
    }, 400);
    return () => clearTimeout(timer);
  }, [egressHosts, connectors]);

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
      <ModalBody flushY constrained>
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
            <Eyebrow>Egress hosts</Eyebrow>
            <EgressHostsEditor value={egressHosts} onChange={setEgressHosts} />
          </Stack>

          <Stack gap="snug">
            <Eyebrow>Connections</Eyebrow>
            <ConnectionCards available={connections} value={connectors} onChange={setConnectors} />
          </Stack>

          <EffectiveAccessPanel
            description="What this environment grants, resolved live as you edit."
            access={access}
            loading={!access && !accessError}
            error={accessError}
          />

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
