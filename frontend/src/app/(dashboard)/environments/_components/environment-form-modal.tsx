'use client';

/** @tier 3 — environments-scoped. Create/edit modal for an environment: name,
 * description, sandbox image, size, egress hosts, connections, and a live
 * effective-access preview. */

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Eyebrow,
  FormField,
  Grid,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  NativeSelect,
  Stack,
  Text,
  Textarea,
} from '@orbit/terra';
import type {
  ConnectionCatalogEntry,
  Connector,
  EffectiveAccess,
  EnvironmentSize,
  EnvironmentSizes,
  SandboxImage,
} from '@orbit/shared/api';
import { ConnectionCards, EffectiveAccessPanel } from '@orbit/components';
import {
  createEnvironment,
  previewEnvironmentAccess,
  updateEnvironment,
} from '../_services/environments-api';
import { EgressHostsEditor } from './egress-hosts-editor';
import { BASE_IMAGE_NAME, imageProblem } from '@orbit/lib/environments';
import type { EnvironmentRow } from '../_types';

interface EnvironmentFormModalProps {
  open: boolean;
  /** When set, the modal edits this environment; otherwise it creates a new one. */
  environment: EnvironmentRow | null;
  connections: ConnectionCatalogEntry[];
  /** Registered sandbox images, or null when the list did not load. */
  images: SandboxImage[] | null;
  /** The platform's default and maximum sizes, or null when they did not load. */
  sizes: EnvironmentSizes | null;
  onClose: () => void;
  onSaved: () => void;
}

const EMPTY_SIZE: EnvironmentSize = { cpu: '', memory: '', disk: '' };

const SIZE_FIELDS: { key: keyof EnvironmentSize; label: string; placeholder: string }[] = [
  { key: 'cpu', label: 'CPU', placeholder: 'e.g. 2' },
  { key: 'memory', label: 'Memory', placeholder: 'e.g. 4Gi' },
  { key: 'disk', label: 'Local disk', placeholder: 'e.g. 10Gi' },
];

export function EnvironmentFormModal({
  open,
  environment,
  connections,
  images,
  sizes,
  onClose,
  onSaved,
}: EnvironmentFormModalProps) {
  const isEdit = environment !== null;

  const [name, setName] = useState(environment?.name ?? '');
  const [description, setDescription] = useState(environment?.description ?? '');
  const [sandboxImage, setSandboxImage] = useState(environment?.sandboxImage ?? BASE_IMAGE_NAME);
  // A new environment starts at the platform default size.
  const [size, setSize] = useState<EnvironmentSize>(
    environment?.size ?? sizes?.default ?? EMPTY_SIZE,
  );
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
      sandbox_image: sandboxImage,
      size: { cpu: size.cpu.trim(), memory: size.memory.trim(), disk: size.disk.trim() },
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
  }, [
    name,
    description,
    sandboxImage,
    size,
    egressHosts,
    connectors,
    isEdit,
    environment,
    onSaved,
  ]);

  const choices = images ?? [];
  const selected = choices.find((i) => i.name === sandboxImage);
  const problem = imageProblem(sandboxImage, images);

  return (
    <Modal open={open} onClose={onClose} size="lg">
      <ModalHeader
        title={isEdit ? 'Edit environment' : 'New environment'}
        description="A sandbox image, a size, and the hosts and connections agents get with it."
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

          <FormField
            id="environment-image"
            label="Sandbox image"
            hint={
              images === null
                ? 'Could not load the registered images.'
                : selected?.description || undefined
            }
            error={problem ?? undefined}
          >
            <NativeSelect
              id="environment-image"
              value={sandboxImage}
              onChange={(e) => setSandboxImage(e.target.value)}
            >
              {choices.map((image) => (
                <option key={image.name} value={image.name} disabled={!image.valid}>
                  {image.valid ? image.name : `${image.name} (not valid)`}
                </option>
              ))}
              {!selected && (
                <option value={sandboxImage} disabled={images !== null}>
                  {images === null ? sandboxImage : `${sandboxImage} (not registered)`}
                </option>
              )}
            </NativeSelect>
          </FormField>

          <Stack gap="snug">
            <Eyebrow>Size</Eyebrow>
            <Grid cols={{ base: 1, sm: 3 }} gap="cozy">
              {SIZE_FIELDS.map((field) => (
                <FormField
                  key={field.key}
                  id={`environment-${field.key}`}
                  label={field.label}
                  hint={sizes ? `Maximum ${sizes.max[field.key]}` : undefined}
                >
                  <Input
                    id={`environment-${field.key}`}
                    value={size[field.key]}
                    onChange={(e) => setSize((prev) => ({ ...prev, [field.key]: e.target.value }))}
                    placeholder={field.placeholder}
                  />
                </FormField>
              ))}
            </Grid>
            <Text size="caption" color="muted">
              {sizes
                ? 'Leave a field empty for the platform default.'
                : 'Could not load the size limits. They are checked when you save.'}
            </Text>
          </Stack>

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
        <Button
          size="sm"
          onClick={() => void handleSubmit()}
          disabled={submitting || !name.trim() || problem !== null}
        >
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
