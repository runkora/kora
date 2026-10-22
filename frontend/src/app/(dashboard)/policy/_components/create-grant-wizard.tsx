'use client';

/** @tier 3 — policy-scoped. Creates a grant for a person: user, scopes, an optional budget, then
 * review. It offers no other principal type: agent, trigger and token grants are written by the
 * agent, trigger or token they belong to, and the policy-service refuses them from anyone but a
 * SYSTEM caller. */

import { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  ButtonGroup,
  FormField,
  Heading,
  Icon,
  Input,
  SectionHeading,
  Stack,
  Text,
  Toggle,
  ToggleRow,
  Wizard,
} from '@runkora/hades';
import { ScopeChipList, ScopePicker } from '@runkora/components';
import { fmtUsd, fmtWindowSuffix } from '@runkora/utils';
import type { PATAttributionEntry } from '../../model-gateway/_services/tokens-server';
import { createGrantAction } from '../_actions/grants';
import type { BudgetAssignment } from '../_types';
import { UserPicker } from './user-picker';

interface CreateGrantWizardProps {
  /** Every scope the grant can carry. */
  availableScopes: string[];
  onClose: () => void;
  /** Called once the grant exists, so the list can refresh behind the success screen. */
  onCreated: () => void;
}

const STEPS = [
  { id: 'user', title: 'User', sub: 'Choose who the grant is for' },
  { id: 'scopes', title: 'Scopes', sub: 'Choose what they can do' },
  { id: 'budget', title: 'Budget', sub: 'Optionally cap their spend' },
  { id: 'review', title: 'Review', sub: 'Check and create the grant' },
];

const USER = 0;
const SCOPES = 1;
const BUDGET = 2;
const REVIEW = 3;

// What the strip says while the step is valid.
const READY = [
  'Ready to continue.',
  'Ready to continue.',
  'Ready to continue. The budget is optional.',
  'Ready to create the grant.',
];

type Cadence = 'weekly' | 'monthly';

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

export function CreateGrantWizard({ availableScopes, onClose, onCreated }: CreateGrantWizardProps) {
  const [step, setStep] = useState(USER);
  const [maxVisited, setMaxVisited] = useState(USER);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [created, setCreated] = useState(false);

  const [userId, setUserId] = useState('');
  const [user, setUser] = useState<PATAttributionEntry | null>(null);
  const [scopes, setScopes] = useState<string[]>([]);
  const [budgetEnabled, setBudgetEnabled] = useState(false);
  const [limitUSD, setLimitUSD] = useState('');
  const [perRunUSD, setPerRunUSD] = useState('');
  const [cadence, setCadence] = useState<Cadence>('monthly');

  const limit = limitUSD === '' ? 0 : parseFloat(limitUSD);
  const perRun = perRunUSD === '' ? 0 : parseFloat(perRunUSD);
  // Each limit is blank or a positive amount, and a budget needs at least one of them.
  const budgetValid =
    Number.isFinite(limit) &&
    limit >= 0 &&
    Number.isFinite(perRun) &&
    perRun >= 0 &&
    limit + perRun > 0;
  const budget = useMemo<BudgetAssignment | null>(
    () =>
      budgetEnabled && budgetValid
        ? {
            reset_cadence: cadence,
            ...(limit > 0 ? { limit_usd: limit } : {}),
            ...(perRun > 0 ? { per_run_limit_usd: perRun } : {}),
          }
        : null,
    [budgetEnabled, budgetValid, limit, perRun, cadence],
  );

  const userName = user ? user.display_name || user.email : userId;

  const hints: (string | undefined)[] = [
    userId ? undefined : 'Choose a user before you continue.',
    scopes.length === 0 ? 'Choose at least one scope before you continue.' : undefined,
    budgetEnabled && !budgetValid
      ? 'Enter a spend limit or a per-run limit, or turn the budget off.'
      : undefined,
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
      const result = await createGrantAction({
        principal_type: 'HUMAN',
        principal_id: userId,
        scopes,
        budget,
      });
      if (!result.ok) {
        setSubmitError(result.message);
        return;
      }
      setCreated(true);
      onCreated();
    } catch {
      // A transport failure reaches here instead of a result; the wizard stays open to retry.
      setSubmitError('Could not create the grant. Check your connection and try again.');
    } finally {
      setSubmitting(false);
    }
  }, [userId, scopes, budget, onCreated]);

  const createAnother = () => {
    setStep(USER);
    setMaxVisited(USER);
    setUserId('');
    setUser(null);
    setScopes([]);
    setBudgetEnabled(false);
    setLimitUSD('');
    setPerRunUSD('');
    setCadence('monthly');
    setSubmitError(null);
    setCreated(false);
  };

  const body = (() => {
    switch (step) {
      case USER:
        return (
          <SectionHeading
            icon="user"
            title="User"
            description="Choose the person this grant is for. Agent, trigger and token grants are managed by the agent, trigger or token they belong to."
          >
            <UserPicker
              value={userId}
              onChange={(id, picked) => {
                setUserId(id);
                setUser(picked);
              }}
            />
          </SectionHeading>
        );
      case SCOPES:
        return (
          <SectionHeading
            icon="shield-check"
            title="Scopes"
            description="Choose what this user is allowed to do."
          >
            <Text size="caption" color="subtle">
              {scopes.length} selected
            </Text>
            <ScopePicker scopes={availableScopes} selected={scopes} onChange={setScopes} />
          </SectionHeading>
        );
      case BUDGET:
        return (
          <SectionHeading
            icon="dollar-sign"
            title="Budget"
            description="Optional. A spend ceiling on this grant."
          >
            <ToggleRow
              label="Budget"
              description="Cap what this grant can spend"
              htmlFor="grant-budget-toggle"
              control={
                <Toggle
                  id="grant-budget-toggle"
                  checked={budgetEnabled}
                  onChange={setBudgetEnabled}
                />
              }
            />
            {budgetEnabled && (
              <Stack gap="comfortable">
                <FormField id="grant-budget-limit" label="Spend limit">
                  <Stack direction="horizontal" align="center" gap="snug">
                    <Input
                      id="grant-budget-limit"
                      type="number"
                      placeholder="e.g. 50"
                      startAdornment="$"
                      value={limitUSD}
                      onChange={(e) => setLimitUSD(e.target.value)}
                    />
                    <ButtonGroup>
                      <Button
                        variant="outline-muted"
                        size="sm"
                        selected={cadence === 'weekly'}
                        onClick={() => setCadence('weekly')}
                      >
                        Weekly
                      </Button>
                      <Button
                        variant="outline-muted"
                        size="sm"
                        selected={cadence === 'monthly'}
                        onClick={() => setCadence('monthly')}
                      >
                        Monthly
                      </Button>
                    </ButtonGroup>
                  </Stack>
                </FormField>
                <FormField
                  id="grant-budget-per-run"
                  label="Per-run limit"
                  hint="Caps what a single agent run started on this person's behalf can spend. Set either limit or both."
                >
                  <Stack direction="horizontal" align="center" gap="snug">
                    <Input
                      id="grant-budget-per-run"
                      type="number"
                      placeholder="e.g. 5"
                      startAdornment="$"
                      value={perRunUSD}
                      onChange={(e) => setPerRunUSD(e.target.value)}
                    />
                    <Text size="caption" color="muted">
                      / run
                    </Text>
                  </Stack>
                </FormField>
              </Stack>
            )}
          </SectionHeading>
        );
      default:
        return (
          <SectionHeading
            icon="rocket"
            title="Review"
            description="Check the details below, then create the grant."
          >
            {submitError && <Alert variant="error">{submitError}</Alert>}
            <Wizard.ReviewStep>
              <Wizard.ReviewCard
                icon="user"
                title="User"
                onEdit={() => goTo(USER)}
                rows={[
                  { label: 'Name', value: userName || 'Not set' },
                  ...(user?.display_name && user.email
                    ? [{ label: 'Email', value: user.email }]
                    : []),
                  { label: 'User ID', value: userId || 'Not set', mono: userId.length > 0 },
                ]}
              />
              <Wizard.ReviewCard
                icon="shield-check"
                title="Scopes"
                onEdit={() => goTo(SCOPES)}
                rows={[{ label: 'Selected', value: plural(scopes.length, 'scope', 'scopes') }]}
              >
                <Stack paddingBlock="snug">
                  <ScopeChipList scopes={scopes} max={8} />
                </Stack>
              </Wizard.ReviewCard>
              <Wizard.ReviewCard
                icon="dollar-sign"
                title="Budget"
                onEdit={() => goTo(BUDGET)}
                rows={[
                  {
                    label: 'Spend limit',
                    value: budget?.limit_usd
                      ? `${fmtUsd(budget.limit_usd)}${fmtWindowSuffix(budget.reset_cadence)}`
                      : 'None',
                  },
                  {
                    label: 'Per-run limit',
                    value: budget?.per_run_limit_usd
                      ? `${fmtUsd(budget.per_run_limit_usd)} per run`
                      : 'None',
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
      icon="shield-check"
      title="Create grant"
      rail={
        <Wizard.Rail
          label="Create grant"
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
          onBack={step > USER ? () => goTo(step - 1) : undefined}
          onNext={() => goTo(step + 1)}
          nextDisabled={hint !== undefined}
          onSubmit={step === REVIEW ? () => void handleSubmit() : undefined}
          submitLabel="Create grant"
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
              <Heading variant="subsection">Grant created</Heading>
              <Text size="supporting" color="muted" align="center">
                <Text as="span" size="supporting" weight="heavy">
                  {userName}
                </Text>{' '}
                now has {plural(scopes.length, 'scope', 'scopes')}.
              </Text>
            </Stack>
            <Stack direction="horizontal" gap="cozy">
              <Button variant="secondary" onClick={onClose}>
                Done
              </Button>
              <Button onClick={createAnother}>
                Create another
                <Icon name="plus" />
              </Button>
            </Stack>
          </Stack>
        </Wizard.Step>
      ) : (
        <Stack grow overflowY="auto" padding="loose" gap="relaxed">
          {/* Always rendered, with the same structure, so a hint appearing or clearing never moves
              the step below it. The Wizard's own hint strip is not used because it mounts only
              while there is a hint. */}
          <Alert
            variant={hint ? 'error' : 'info'}
            role={hint ? 'alert' : 'status'}
            data-slot="grant-wizard-hint"
          >
            <Icon name={hint ? 'alert-circle' : 'info'} size="xs" label="" />
            <Text size="caption" color="muted">
              {hint ?? READY[step]}
            </Text>
          </Alert>
          {body}
        </Stack>
      )}
    </Wizard>
  );
}
