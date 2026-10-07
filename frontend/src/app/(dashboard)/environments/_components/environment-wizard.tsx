'use client';

/** @tier 3 — environments-scoped. Create/edit wizard for an environment: basics,
 * sandbox image and size, egress hosts and connections, then a review. */

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Eyebrow,
  FormField,
  Grid,
  Input,
  NativeSelect,
  SectionHeading,
  Stack,
  Text,
  Textarea,
  Wizard,
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

interface EnvironmentWizardProps {
  /** When set, the wizard edits this environment; otherwise it creates a new one. */
  environment: EnvironmentRow | null;
  connections: ConnectionCatalogEntry[];
  /** Registered sandbox images, or null when the list did not load. */
  images: SandboxImage[] | null;
  /** The platform's default and maximum sizes, or null when they did not load. */
  sizes: EnvironmentSizes | null;
  onClose: () => void;
  onSaved: () => void;
}

const BASICS = 0;
const SANDBOX = 1;
const ACCESS = 2;
const REVIEW = 3;

const STEPS = [
  { id: 'basics', title: 'Basics', sub: 'Name and description' },
  { id: 'sandbox', title: 'Sandbox', sub: 'Image and size' },
  { id: 'access', title: 'Access', sub: 'Egress hosts and connections' },
  { id: 'review', title: 'Review', sub: 'Confirm and save' },
];

const EMPTY_SIZE: EnvironmentSize = { cpu: '', memory: '', disk: '' };

const SIZE_FIELDS: { key: keyof EnvironmentSize; label: string; placeholder: string }[] = [
  { key: 'cpu', label: 'CPU', placeholder: 'e.g. 2' },
  { key: 'memory', label: 'Memory', placeholder: 'e.g. 4Gi' },
  { key: 'disk', label: 'Local disk', placeholder: 'e.g. 10Gi' },
];

/** The step that owns the field an API validation error names; anything else stays on review. */
export function stepForError(message: string): number {
  if (/: (connectivity|connector) /.test(message)) return ACCESS;
  if (/: (size\.|sandbox[ _]image)/.test(message)) return SANDBOX;
  if (/name/.test(message)) return BASICS;
  return REVIEW;
}

export function EnvironmentWizard({
  environment,
  connections,
  images,
  sizes,
  onClose,
  onSaved,
}: EnvironmentWizardProps) {
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

  const [step, setStep] = useState(BASICS);
  // Edit opens with every step reachable: an existing environment has every field filled.
  const [maxVisited, setMaxVisited] = useState(isEdit ? REVIEW : BASICS);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<{ step: number; message: string } | null>(null);

  // Debounced so typing in the hosts editor doesn't spam the preview endpoint.
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

  const choices = images ?? [];
  const selected = choices.find((i) => i.name === sandboxImage);
  const problem = imageProblem(sandboxImage, images);
  const nameMissing = !name.trim();

  const hints: (string | undefined)[] = [
    nameMissing ? 'Enter a name before continuing.' : undefined,
    problem ? 'Choose a valid sandbox image before continuing.' : undefined,
    undefined,
    nameMissing ? 'Basics needs a name.' : problem ? 'Sandbox needs a valid image.' : undefined,
  ];
  const hint = hints[step];

  const goTo = useCallback(
    (target: number) => {
      if (submitting) return;
      setStep(target);
      setMaxVisited((m) => Math.max(m, target));
      setSubmitError(null);
    },
    [submitting],
  );

  const handleSubmit = useCallback(async () => {
    setSubmitting(true);
    setSubmitError(null);
    const payload = {
      name: name.trim(),
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
      const target = stepForError(result.error);
      setStep(target);
      setSubmitError({ step: target, message: result.error });
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

  const errorAlert =
    submitError && submitError.step === step ? (
      <Alert variant="error">{submitError.message}</Alert>
    ) : null;

  const sizeValue = (key: keyof EnvironmentSize) => size[key].trim() || 'Platform default';

  return (
    <Wizard
      open
      onClose={submitting ? () => undefined : onClose}
      icon="layers"
      title={isEdit ? 'Edit environment' : 'New environment'}
      hint={hint}
      rail={
        <Wizard.Rail
          label={isEdit ? 'Edit environment' : 'Create environment'}
          steps={STEPS}
          current={step}
          maxVisited={maxVisited}
          onGoTo={goTo}
        />
      }
      footer={
        <Wizard.Footer
          step={step}
          totalSteps={STEPS.length}
          onBack={step > BASICS ? () => goTo(step - 1) : undefined}
          onNext={() => goTo(step + 1)}
          nextDisabled={hint !== undefined}
          onSubmit={step === REVIEW ? () => void handleSubmit() : undefined}
          submitLabel={isEdit ? 'Save changes' : 'Create environment'}
          pending={submitting}
          pendingLabel={isEdit ? 'Saving…' : 'Creating…'}
        />
      }
    >
      {step === BASICS && (
        <Wizard.Step>
          <SectionHeading
            icon="layers"
            title="Basics"
            description="Name the environment and say what it grants."
          >
            {errorAlert}
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
          </SectionHeading>
        </Wizard.Step>
      )}

      {step === SANDBOX && (
        <Wizard.Step>
          <SectionHeading
            icon="box"
            title="Sandbox"
            description="The image the sandbox runs and the resources it gets."
          >
            {errorAlert}
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
                      onChange={(e) =>
                        setSize((prev) => ({ ...prev, [field.key]: e.target.value }))
                      }
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
          </SectionHeading>
        </Wizard.Step>
      )}

      {step === ACCESS && (
        <Wizard.Step>
          <SectionHeading
            icon="globe"
            title="Access"
            description="The hosts and connections agents get with this environment."
          >
            {errorAlert}
            <Stack gap="snug">
              <Eyebrow>Egress hosts</Eyebrow>
              <EgressHostsEditor value={egressHosts} onChange={setEgressHosts} />
            </Stack>
            <Stack gap="snug">
              <Eyebrow>Connections</Eyebrow>
              <ConnectionCards
                available={connections}
                value={connectors}
                onChange={setConnectors}
              />
            </Stack>
            <EffectiveAccessPanel
              description="What this environment grants, resolved live as you edit."
              access={access}
              loading={!access && !accessError}
              error={accessError}
            />
          </SectionHeading>
        </Wizard.Step>
      )}

      {step === REVIEW && (
        <Wizard.Step>
          <SectionHeading
            icon="rocket"
            title="Review"
            description={
              isEdit
                ? 'Confirm the changes and save them.'
                : 'Confirm the environment and create it. Nothing exists until you do.'
            }
          />
          {errorAlert}
          <Wizard.ReviewStep>
            <Wizard.ReviewCard
              icon="layers"
              title="Basics"
              onEdit={() => goTo(BASICS)}
              rows={[
                { label: 'Name', value: name.trim() || 'Not set' },
                { label: 'Description', value: description.trim() || 'None' },
              ]}
            />
            <Wizard.ReviewCard
              icon="box"
              title="Sandbox"
              onEdit={() => goTo(SANDBOX)}
              rows={[
                { label: 'Sandbox image', value: sandboxImage, mono: true },
                { label: 'CPU', value: sizeValue('cpu'), mono: true },
                { label: 'Memory', value: sizeValue('memory'), mono: true },
                { label: 'Local disk', value: sizeValue('disk'), mono: true },
              ]}
            />
            <Wizard.ReviewCard
              icon="globe"
              title="Access"
              onEdit={() => goTo(ACCESS)}
              rows={[
                { label: 'Egress hosts', value: egressHosts.join(', ') || 'None' },
                {
                  label: 'Connections',
                  value: connectors.map((c) => c.connection).join(', ') || 'None',
                },
              ]}
            />
          </Wizard.ReviewStep>
        </Wizard.Step>
      )}
    </Wizard>
  );
}
