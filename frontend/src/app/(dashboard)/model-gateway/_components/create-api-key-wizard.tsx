'use client';

/** @tier 3 — model-gateway-scoped. Creates an API key (a personal access token): name and expiry,
 * scopes, then review. The success step shows the key once; it cannot be read back afterwards. */

import { useCallback, useState } from 'react';
import {
  Alert,
  Button,
  CopyField,
  FormField,
  Heading,
  Icon,
  Input,
  SectionHeading,
  Stack,
  Text,
  Wizard,
} from '@runkora/hades';
import { ScopeChipList, ScopePicker } from '@runkora/components';
import type { ScopeOption } from '@runkora/components';
import type { CreatePATResponse } from '@runkora/shared/api';
import { createToken } from '../_actions/tokens';

interface CreateApiKeyWizardProps {
  /** Every scope the key can carry; a key cannot exceed the creator's own access. */
  availableScopes: string[];
  /** The vocabulary entries behind `availableScopes`; the picker groups by them and shows descriptions. */
  scopeOptions?: ScopeOption[];
  onClose: () => void;
  /** Called once the key exists, so the list can refresh behind the success screen. */
  onCreated: () => void;
}

const STEPS = [
  { id: 'details', title: 'Details', sub: 'Name the key and set its expiry' },
  { id: 'scopes', title: 'Permissions', sub: 'Choose what it can do' },
  { id: 'review', title: 'Review', sub: 'Check and create the key' },
];

const DETAILS = 0;
const SCOPES = 1;
const REVIEW = 2;

const MAX_NAME = 128;
const MAX_EXPIRY_DAYS = 365;
const DEFAULT_EXPIRY_DAYS = '90';

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

const fmtLongDate = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });

/** An empty expiry means the server default; anything else must be a whole number of days in range. */
function expiryProblem(expiresInDays: string): string | null {
  if (expiresInDays.trim() === '') return null;
  const days = Number(expiresInDays);
  if (!Number.isInteger(days) || days < 1 || days > MAX_EXPIRY_DAYS) {
    return `Expiry must be between 1 and ${MAX_EXPIRY_DAYS} days.`;
  }
  return null;
}

export function CreateApiKeyWizard({
  availableScopes,
  scopeOptions,
  onClose,
  onCreated,
}: CreateApiKeyWizardProps) {
  const [step, setStep] = useState(DETAILS);
  const [maxVisited, setMaxVisited] = useState(DETAILS);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatePATResponse | null>(null);

  const [name, setName] = useState('');
  const [expiresInDays, setExpiresInDays] = useState(DEFAULT_EXPIRY_DAYS);
  const [scopes, setScopes] = useState<string[]>([]);

  const trimmedName = name.trim();
  const detailsProblem = !trimmedName
    ? 'Name the key before you continue.'
    : (expiryProblem(expiresInDays) ?? undefined);

  const hints: (string | undefined)[] = [
    detailsProblem,
    scopes.length === 0 ? 'Choose at least one permission before you continue.' : undefined,
    undefined,
  ];
  // Review stands for every step before it, so it cannot be reached with one of them invalid.
  const hint = step === REVIEW ? (hints.find((h) => h !== undefined) ?? undefined) : hints[step];

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
    try {
      const result = await createToken({
        name: trimmedName,
        expires_in_days: expiresInDays.trim() === '' ? undefined : Number(expiresInDays),
        scopes,
      });
      if (!result.ok) {
        setSubmitError(result.message);
        return;
      }
      setCreated(result.pat);
      onCreated();
    } catch {
      // A transport failure reaches here instead of a result; the wizard stays open to retry.
      setSubmitError('Could not create the key. Check your connection and try again.');
    } finally {
      setSubmitting(false);
    }
  }, [trimmedName, expiresInDays, scopes, onCreated]);

  const createAnother = () => {
    setStep(DETAILS);
    setMaxVisited(DETAILS);
    setName('');
    setExpiresInDays(DEFAULT_EXPIRY_DAYS);
    setScopes([]);
    setSubmitError(null);
    setCreated(null);
  };

  const expiryLabel =
    expiresInDays.trim() === '' ? 'Server default' : plural(Number(expiresInDays), 'day', 'days');

  const body = (() => {
    switch (step) {
      case DETAILS:
        return (
          <SectionHeading
            icon="key"
            title="Details"
            description="Keys are scoped to your account and usable at inference endpoints."
          >
            <FormField
              id="api-key-name"
              label="Name"
              required
              hint={`A short label to identify this key. Max ${MAX_NAME} characters.`}
            >
              <Input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Production Agent Fleet"
                maxLength={MAX_NAME}
                autoFocus
              />
            </FormField>
            <FormField
              id="api-key-expiry"
              label="Expires in (days)"
              hint={`Defaults to ${DEFAULT_EXPIRY_DAYS} days. Maximum ${MAX_EXPIRY_DAYS} days.`}
            >
              <Input
                type="number"
                value={expiresInDays}
                onChange={(e) => setExpiresInDays(e.target.value)}
                min={1}
                max={MAX_EXPIRY_DAYS}
                placeholder={DEFAULT_EXPIRY_DAYS}
              />
            </FormField>
          </SectionHeading>
        );
      case SCOPES:
        return (
          <SectionHeading
            icon="shield-check"
            title="Permissions"
            description="Choose which permissions this key carries. Keys cannot exceed your own access."
          >
            {availableScopes.length === 0 ? (
              <Text size="supporting" color="muted">
                No scopes available. Your account has no granted permissions.
              </Text>
            ) : (
              <ScopePicker
                options={scopeOptions}
                scopes={availableScopes}
                selected={scopes}
                onChange={setScopes}
              />
            )}
          </SectionHeading>
        );
      default:
        return (
          <SectionHeading
            icon="rocket"
            title="Review"
            description="Check the details below, then create the key."
          >
            {submitError && <Alert variant="error">{submitError}</Alert>}
            <Wizard.ReviewStep>
              <Wizard.ReviewCard
                icon="key"
                title="Details"
                onEdit={() => goTo(DETAILS)}
                rows={[
                  { label: 'Name', value: trimmedName || 'Not set' },
                  { label: 'Expires in', value: expiryLabel },
                ]}
              />
              <Wizard.ReviewCard
                icon="shield-check"
                title="Permissions"
                onEdit={() => goTo(SCOPES)}
                rows={[{ label: 'Selected', value: plural(scopes.length, 'scope', 'scopes') }]}
              >
                <Stack paddingBlock="snug">
                  <ScopeChipList scopes={scopes} max={8} />
                </Stack>
              </Wizard.ReviewCard>
            </Wizard.ReviewStep>
          </SectionHeading>
        );
    }
  })();

  return (
    <Wizard
      open
      // The key is shown once, on the success step. Only its buttons close it, so an
      // outside click cannot discard a key the user has not copied.
      onClose={submitting || created ? () => undefined : onClose}
      icon="key"
      title="Create API key"
      hint={created ? undefined : hint}
      rail={
        <Wizard.Rail
          label="Create API key"
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
          onBack={step > DETAILS ? () => goTo(step - 1) : undefined}
          onNext={() => goTo(step + 1)}
          nextDisabled={hint !== undefined}
          onSubmit={step === REVIEW ? () => void handleSubmit() : undefined}
          submitLabel="Create key"
          pending={submitting}
        />
      }
    >
      {created ? (
        <Wizard.Step fullPage>
          <Stack
            grow
            align="center"
            justify="center"
            gap="comfortable"
            padding="loose"
            overflowY="auto"
          >
            <Stack align="center" gap="snug">
              <Icon
                name="check-circle"
                size="xl"
                color="accent"
                container
                containerVariant="accent"
                circle
                label=""
              />
              <Heading variant="subsection">API key created</Heading>
              <Text size="supporting" color="muted" align="center">
                {created.name}
              </Text>
            </Stack>
            {/* The token is a ~1,000 character JWT. Without a bound, its unbreakable text sets the
                width of this centred column instead of truncating inside the copy field. */}
            <Stack gap="cozy" maxWidth={{ base: 'full', sm: 'document' }}>
              <Alert variant="warning">
                <Icon name="triangle-alert" color="status-degraded" />
                <Stack gap="tight">
                  <Text size="supporting" color="foreground" weight="heavy">
                    Copy this key now.
                  </Text>
                  <Text size="supporting" color="foreground">
                    It will not be shown again. If you lose it, you will need to revoke it and
                    create a new one.
                  </Text>
                </Stack>
              </Alert>
              <Stack gap="tight">
                <Text size="caption" color="subtle" weight="emphasis">
                  Your API key
                </Text>
                <CopyField
                  key={created.token}
                  value={created.token}
                  ariaLabel="Copy API key to clipboard"
                  mono
                />
                <Text size="caption" color="subtle">
                  {created.expires_at
                    ? `Expires ${fmtLongDate(created.expires_at)}`
                    : 'Never expires'}
                </Text>
              </Stack>
            </Stack>
            <Stack direction="horizontal" gap="cozy" wrap justify="center">
              <Button variant="secondary" onClick={createAnother}>
                Create another
                <Icon name="plus" />
              </Button>
              <Button onClick={onClose}>Done, I&apos;ve copied it</Button>
            </Stack>
          </Stack>
        </Wizard.Step>
      ) : (
        <Stack grow overflowY="auto" padding="loose" gap="relaxed">
          {body}
        </Stack>
      )}
    </Wizard>
  );
}
