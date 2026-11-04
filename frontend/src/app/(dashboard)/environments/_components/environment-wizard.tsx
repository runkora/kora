'use client';

/** @tier 3 — environments-scoped. Create/edit wizard for an environment. Create starts
 * from a sandbox image or a copy of an environment, and edit starts on the name. Both
 * continue to the sandbox image and size, the access, and a review. */

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Badge,
  Card,
  CardContent,
  CardHeader,
  Eyebrow,
  FormField,
  Grid,
  Icon,
  Input,
  RadioGroup,
  RadioGroupItem,
  SectionHeading,
  Stack,
  Text,
  Textarea,
  Wizard,
} from '@runkora/hades';
import type {
  ConnectionCatalogEntry,
  Connector,
  EffectiveAccess,
  EnvironmentRequest,
  EnvironmentSizes,
  SandboxImage,
} from '@runkora/shared/api';
import { ConnectionCards, EffectiveAccessPanel } from '@runkora/components';
import { BASE_IMAGE_NAME, imageProblem, slugProblem } from '@runkora/lib/environments';
import { deriveSlug, slugInput } from '@runkora/lib/slug';
import {
  createEnvironment,
  previewEnvironmentAccess,
  updateEnvironment,
} from '../_services/environments-api';
import type { EnvironmentRow } from '../_types';
import { EgressHostsEditor } from './egress-hosts-editor';
import {
  sizeChoiceFor,
  sizeInvalid,
  sizeRequest,
  sizeSummary,
  type SizeChoice,
} from './environment-size';
import { ImageChoice, ImageTable, SANDBOX_INTRO, SizeCard } from './sandbox-step';

interface EnvironmentWizardProps {
  /** When set, the wizard edits this environment; otherwise it creates a new one. */
  environment: EnvironmentRow | null;
  /** The tenant's environments, offered for copying. */
  environments: EnvironmentRow[];
  connections: ConnectionCatalogEntry[];
  /** Registered sandbox images, or null when the list did not load. */
  images: SandboxImage[] | null;
  /** The platform's default and maximum sizes, or null when they did not load. */
  sizes: EnvironmentSizes | null;
  onClose: () => void;
  onSaved: () => void;
}

const STEPS = [
  { id: 'start', title: 'Start', sub: 'Choose a starting point' },
  { id: 'sandbox', title: 'Sandbox', sub: 'Choose an image and size' },
  { id: 'access', title: 'Access', sub: 'Choose what agents can reach' },
  { id: 'review', title: 'Review', sub: 'Check and create the environment' },
];

// An existing environment has nothing to start from, so its first step is the name.
const EDIT_STEPS = [
  { id: 'identity', title: 'Identity', sub: 'Name and description' },
  ...STEPS.slice(1),
];

const START = 0;
const SANDBOX = 1;
const ACCESS = 2;
const REVIEW = 3;

type StartFrom = 'image' | 'copy';

/** A readable name from an image name: drop the org prefix, keep the rest. acme-go becomes Go. */
export function nameFromImage(image: string): string {
  const rest = image.includes('-') ? image.slice(image.indexOf('-') + 1) : image;
  const words = rest.replace(/-/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** The step that owns the field an API validation error names; anything else stays on review. */
export function stepForError(message: string): number {
  if (/: (connectivity|connector) /.test(message)) return ACCESS;
  if (/: (size\.|sandbox[ _]image)/.test(message)) return SANDBOX;
  if (/name|slug/.test(message)) return START;
  return REVIEW;
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

export function EnvironmentWizard({
  environment,
  environments,
  connections,
  images,
  sizes,
  onClose,
  onSaved,
}: EnvironmentWizardProps) {
  const isEdit = environment !== null;

  const [step, setStep] = useState(START);
  // Edit opens with every step reachable: an existing environment has every field filled.
  const [maxVisited, setMaxVisited] = useState(isEdit ? REVIEW : START);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<{ step: number; message: string } | null>(null);

  const [image, setImage] = useState(environment?.sandboxImage ?? BASE_IMAGE_NAME);
  const [size, setSize] = useState<SizeChoice | null>(() =>
    sizes ? sizeChoiceFor(environment?.size, sizes) : null,
  );
  const [egressHosts, setEgressHosts] = useState<string[]>(environment?.egressHosts ?? []);
  const [connectors, setConnectors] = useState<Connector[]>(environment?.connectors ?? []);

  const [start, setStart] = useState<StartFrom>('image');
  const [copyFrom, setCopyFrom] = useState('');

  // The name and description follow the image until someone types in them.
  const imageInfo = images?.find((i) => i.name === image);
  const [nameTyped, setNameTyped] = useState(environment?.name);
  const [descriptionTyped, setDescriptionTyped] = useState(environment?.description);
  const name = nameTyped ?? nameFromImage(image);
  const description = descriptionTyped ?? imageInfo?.description ?? '';
  // As in the agent builder, the slug follows the name until someone types in it.
  const [slugTyped, setSlugTyped] = useState<string | undefined>(environment?.slug);
  const slug = slugTyped ?? deriveSlug(name);

  // Copying an environment fills everything it has.
  const copyEnvironment = (id: string) => {
    const env = environments.find((e) => e.id === id);
    if (!env) return;
    setCopyFrom(id);
    setNameTyped(`${env.name} copy`);
    setSlugTyped(undefined);
    setDescriptionTyped(env.description);
    setImage(env.sandboxImage);
    if (sizes) setSize(sizeChoiceFor(env.size, sizes));
    setEgressHosts(env.egressHosts);
    setConnectors(env.connectors);
  };

  // Going back to an image drops what a copy filled in.
  const chooseStart = (next: StartFrom) => {
    setStart(next);
    if (next === 'copy' || copyFrom === '') return;
    setCopyFrom('');
    setNameTyped(undefined);
    setSlugTyped(undefined);
    setDescriptionTyped(undefined);
    setImage(BASE_IMAGE_NAME);
    if (sizes) setSize(sizeChoiceFor(undefined, sizes));
    setEgressHosts([]);
    setConnectors([]);
  };

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

  const problem = imageProblem(image, images);
  const nameMissing = !name.trim();
  // A saved slug is locked, and one that predates the slug rule stays valid.
  const slugError = isEdit ? null : slugProblem(slug);
  const badSize = size !== null && sizes !== null && sizeInvalid(size, sizes);

  const identityHint = nameMissing
    ? 'Enter a name before you continue.'
    : slugError
      ? 'Fix the slug before you continue.'
      : undefined;
  const copyMissing = !isEdit && start === 'copy' && copyFrom === '';
  const sandboxHint = problem
    ? 'Choose a valid image.'
    : badSize
      ? 'Fix the size before you continue.'
      : undefined;
  const hints: (string | undefined)[] = [
    // Edit shows no image on its first step, so only the name and slug hold it.
    identityHint ??
      (copyMissing
        ? 'Choose an environment to copy.'
        : problem && !isEdit
          ? 'Choose a valid image.'
          : undefined),
    sandboxHint,
    undefined,
    identityHint ?? (copyMissing ? 'Choose an environment to copy.' : sandboxHint),
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
    const payload: EnvironmentRequest = {
      slug,
      name: name.trim(),
      description: description.trim(),
      sandbox_image: image,
      // Without the size limits the wizard keeps the saved or copied size, or the platform default.
      size:
        size && sizes
          ? sizeRequest(size, sizes)
          : (environment?.size ??
            environments.find((e) => e.id === copyFrom)?.size ?? { cpu: '', memory: '', disk: '' }),
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
    slug,
    name,
    description,
    image,
    size,
    sizes,
    egressHosts,
    connectors,
    isEdit,
    environment,
    environments,
    copyFrom,
    onSaved,
  ]);

  const errorAlert =
    submitError && submitError.step === step ? (
      <Alert variant="error">{submitError.message}</Alert>
    ) : null;

  // Styled like the Size card: header with icon, title and one line, then the fields.
  const identityCard = (
    <Card subtle>
      <CardHeader
        compact
        divider
        leadingIcon="layers"
        title="Name and description"
        description="Agents use these to decide when to use this environment."
      />
      <CardContent padded>
        <Stack gap="comfortable">
          <Grid cols={2} gap="comfortable">
            <FormField
              id="environment-name"
              label="Name"
              required
              error={nameTyped !== undefined && nameMissing ? 'Enter a name.' : undefined}
            >
              <Input
                id="environment-name"
                value={name}
                onChange={(e) => setNameTyped(e.target.value)}
                placeholder="Go services"
                maxLength={128}
                hasError={nameTyped !== undefined && nameMissing}
              />
            </FormField>
            <FormField
              id="environment-slug"
              label="Slug"
              required
              hint={
                isEdit ? 'Agents select the environment by its slug. It cannot change.' : undefined
              }
              error={slugError && !nameMissing ? slugError : undefined}
            >
              <Input
                id="environment-slug"
                mono
                value={slug}
                readOnly={isEdit}
                onChange={(e) => {
                  if (!isEdit) setSlugTyped(slugInput(e.target.value));
                }}
                placeholder="go-services"
                hasError={slugError !== null && !nameMissing}
              />
            </FormField>
          </Grid>
          <FormField
            id="environment-description"
            label="Description"
            hint="Optional. Agents read this when choosing an environment. Mention the repositories or tasks it is meant for."
          >
            <Textarea
              id="environment-description"
              value={description}
              onChange={(e) => setDescriptionTyped(e.target.value)}
              placeholder="Go services with buf and golangci-lint."
              rows={2}
            />
          </FormField>
        </Stack>
      </CardContent>
    </Card>
  );

  const choiceCard = (value: StartFrom, title: string, text: string, icon: 'box' | 'copy') => (
    <Card interactive selected={start === value} onClick={() => chooseStart(value)} column>
      <CardContent padded compact grow>
        <Stack direction="horizontal" align="start" gap="cozy">
          <Icon
            name={icon}
            size="sm"
            color={start === value ? 'accent' : 'muted'}
            container
            containerVariant={start === value ? 'accent' : 'surface-subtle'}
            label=""
          />
          <Stack gap="tight" grow>
            <Text as="span" size="supporting" weight="heavy">
              {title}
            </Text>
            <Text size="micro" color="muted">
              {text}
            </Text>
          </Stack>
          <RadioGroupItem value={value} aria-label={title} />
        </Stack>
      </CardContent>
    </Card>
  );

  const copyList =
    environments.length === 0 ? (
      <Text size="caption" color="muted">
        There are no environments to copy yet.
      </Text>
    ) : (
      <RadioGroup value={copyFrom} onValueChange={copyEnvironment} aria-label="Environment to copy">
        <Stack gap="snug">
          {environments.map((env) => (
            <Card
              key={env.id}
              interactive
              selected={copyFrom === env.id}
              onClick={() => copyEnvironment(env.id)}
            >
              <CardContent padded compact>
                <Stack direction="horizontal" align="center" gap="cozy">
                  <RadioGroupItem value={env.id} aria-label={env.name} />
                  <Stack gap="none" grow minWidth="none">
                    <Text as="span" size="supporting" weight="heavy">
                      {env.name}
                    </Text>
                    {env.description && (
                      <Text size="caption" color="muted" truncate>
                        {env.description}
                      </Text>
                    )}
                  </Stack>
                  <Text as="span" size="micro" mono color="subtle">
                    {env.sandboxImage}
                  </Text>
                  <Badge variant="elevated" size="sm">
                    {plural(env.egressHosts.length, 'host', 'hosts')} ·{' '}
                    {plural(env.connectors.length, 'connection', 'connections')}
                  </Badge>
                </Stack>
              </CardContent>
            </Card>
          ))}
        </Stack>
      </RadioGroup>
    );

  const scoped = connectors.filter((c) => (c.resources?.length ?? 0) > 0).length;

  const body = (() => {
    switch (step) {
      case START:
        return (
          <SectionHeading
            icon="layers"
            title={isEdit ? 'Identity' : 'Start'}
            description={
              isEdit
                ? undefined
                : 'Start from a sandbox image, or copy an environment you already have.'
            }
          >
            {errorAlert}
            {!isEdit && (
              <>
                <RadioGroup
                  value={start}
                  onValueChange={(v) => chooseStart(v as StartFrom)}
                  aria-label="Start from"
                >
                  <Grid cols={{ base: 1, sm: 2 }} gap="cozy">
                    {choiceCard(
                      'image',
                      'Start from an image',
                      'Choose a sandbox image and set up the rest yourself.',
                      'box',
                    )}
                    {choiceCard(
                      'copy',
                      'Copy an environment',
                      "Reuse an environment's image, size and access, then change what you need.",
                      'copy',
                    )}
                  </Grid>
                </RadioGroup>
                {start === 'image' ? (
                  images === null ? (
                    <Text size="caption" color="muted">
                      Could not load the registered images.
                    </Text>
                  ) : (
                    <ImageTable images={images} value={image} onChange={setImage} />
                  )
                ) : (
                  copyList
                )}
              </>
            )}
            {identityCard}
          </SectionHeading>
        );
      case SANDBOX:
        return (
          <SectionHeading icon="box" title="Sandbox" description={SANDBOX_INTRO}>
            {errorAlert}
            <ImageChoice images={images} value={image} onChange={setImage} />
            {size && sizes ? (
              <SizeCard sizes={sizes} value={size} onChange={setSize} />
            ) : (
              <Text size="caption" color="muted">
                Could not load the size limits. They are checked when you save.
              </Text>
            )}
          </SectionHeading>
        );
      case ACCESS:
        return (
          <SectionHeading
            icon="globe"
            title="Access"
            description="Choose the hosts and connections agents can reach from this environment."
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
              description="Everything agents can reach with this environment."
              access={access}
              loading={!access && !accessError}
              error={accessError}
            />
          </SectionHeading>
        );
      default:
        return (
          <SectionHeading
            icon="rocket"
            title="Review"
            description={
              isEdit
                ? 'Check the details below, then save your changes.'
                : 'Check the details below, then create the environment.'
            }
          >
            {errorAlert}
            <Wizard.ReviewStep>
              <Wizard.ReviewCard
                icon="layers"
                title="Name and description"
                onEdit={() => goTo(START)}
                rows={[
                  { label: 'Name', value: name.trim() || 'Not set' },
                  { label: 'Slug', value: slug || 'Not set', mono: slug.length > 0 },
                  { label: 'Description', value: description.trim() || 'None' },
                ]}
              />
              <Wizard.ReviewCard
                icon="box"
                title="Sandbox"
                onEdit={() => goTo(SANDBOX)}
                rows={[
                  { label: 'Sandbox image', value: image, mono: true },
                  {
                    label: 'Size',
                    value: size && sizes ? sizeSummary(size, sizes) : 'Platform default',
                  },
                ]}
              />
              <Wizard.ReviewCard
                icon="globe"
                title="Access"
                onEdit={() => goTo(ACCESS)}
                rows={[
                  { label: 'Egress hosts', value: String(egressHosts.length) },
                  {
                    label: 'Connections',
                    value:
                      scoped > 0
                        ? `${connectors.length} (${scoped} resource-scoped)`
                        : String(connectors.length),
                  },
                ]}
              />
            </Wizard.ReviewStep>
          </SectionHeading>
        );
    }
  })();

  return (
    <Wizard
      open
      onClose={submitting ? () => undefined : onClose}
      icon="layers"
      title={isEdit ? 'Edit environment' : 'New environment'}
      subtitle={environment?.name}
      hint={hint}
      rail={
        <Wizard.Rail
          label={isEdit ? 'Edit environment' : 'Create environment'}
          steps={isEdit ? EDIT_STEPS : STEPS}
          current={step}
          maxVisited={maxVisited}
          onGoTo={goTo}
        />
      }
      footer={
        <Wizard.Footer
          step={step}
          totalSteps={STEPS.length}
          onBack={step > START ? () => goTo(step - 1) : undefined}
          onNext={() => goTo(step + 1)}
          nextDisabled={hint !== undefined}
          onSubmit={step === REVIEW ? () => void handleSubmit() : undefined}
          submitLabel={isEdit ? 'Save changes' : 'Create environment'}
          pending={submitting}
          pendingLabel={isEdit ? 'Saving…' : 'Creating…'}
        />
      }
    >
      {/* Full content width so the image table fits its columns. */}
      <Stack grow overflowY="auto" padding="loose" gap="relaxed">
        {body}
      </Stack>
    </Wizard>
  );
}
