'use client';

/** @tier 3 — policy-scoped. Creates a role: name, scopes, an optional budget, then review. Editing
 * an existing role stays in the roles tab's edit modal. */

import { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  FormField,
  Heading,
  Icon,
  Input,
  SectionHeading,
  Stack,
  Text,
  Wizard,
} from '@runkora/hades';
import {
  BudgetLimitFields,
  EMPTY_BUDGET_LIMIT_VALUES,
  ScopeChipList,
  ScopePicker,
  budgetFromLimitValues,
  budgetLimitProblem,
  budgetLimitsAnyOn,
} from '@runkora/components';
import type { BudgetLimitValues, ScopeOption } from '@runkora/components';
import { fmtUsd } from '@runkora/utils';
import { upsertRoleAction } from '../_actions/roles';

interface CreateRoleWizardProps {
  /** Every scope the role can carry. */
  availableScopes: string[];
  /** The vocabulary entries behind `availableScopes`; the picker groups by them and shows descriptions. */
  scopeOptions?: ScopeOption[];
  /** Set when the scope vocabulary failed to load; scopes cannot be chosen without it. */
  scopesError?: string | null;
  /** Names already in use. Saving a role under one of them replaces that role, so they are refused. */
  existingRoleNames: string[];
  onClose: () => void;
  /** Called once the role exists, so the list can refresh behind the success screen. */
  onCreated: () => void;
}

const STEPS = [
  { id: 'name', title: 'Name', sub: 'Name the role' },
  { id: 'scopes', title: 'Scopes', sub: 'Choose what it grants' },
  { id: 'budget', title: 'Budget', sub: 'Optionally cap spend' },
  { id: 'review', title: 'Review', sub: 'Check and create the role' },
];

const NAME = 0;
const SCOPES = 1;
const BUDGET = 2;
const REVIEW = 3;

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

export function CreateRoleWizard({
  availableScopes,
  scopeOptions,
  scopesError = null,
  existingRoleNames,
  onClose,
  onCreated,
}: CreateRoleWizardProps) {
  const [step, setStep] = useState(NAME);
  const [maxVisited, setMaxVisited] = useState(NAME);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [created, setCreated] = useState(false);

  const [roleName, setRoleName] = useState('');
  const [scopes, setScopes] = useState<string[]>([]);
  const [limits, setLimits] = useState<BudgetLimitValues>(EMPTY_BUDGET_LIMIT_VALUES);

  const name = roleName.trim();
  const nameTaken = existingRoleNames.some((n) => n.toLowerCase() === name.toLowerCase());

  // A role budget is a spend limit, so the spend row off means no budget.
  const budgetAsked = budgetLimitsAnyOn(limits, 'role');
  const budget = useMemo(() => budgetFromLimitValues(limits, 'role'), [limits]);
  const budgetProblem = budgetAsked ? budgetLimitProblem(limits, 'role') : null;

  const budgetRows = budget?.limit_usd
    ? [
        {
          label: 'Spend limit',
          value: `${fmtUsd(budget.limit_usd)} per ${budget.reset_cadence === 'weekly' ? 'week' : 'month'}`,
        },
        { label: 'Applies to', value: budget.pooled ? 'One pool for all members' : 'Each member' },
      ]
    : [{ label: 'Budget', value: 'No budget' }];

  const nameHint = !name
    ? 'Name the role before you continue.'
    : nameTaken
      ? `A role named "${name}" already exists. Choose a different name.`
      : undefined;

  const hints: (string | undefined)[] = [
    nameHint,
    scopes.length === 0 ? 'Choose at least one scope before you continue.' : undefined,
    budgetProblem ?? undefined,
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
      const result = await upsertRoleAction({ role: name, scopes, budget });
      if (!result.ok) {
        setSubmitError(result.message);
        return;
      }
      setCreated(true);
      onCreated();
    } catch {
      // A transport failure reaches here instead of a result; the wizard stays open to retry.
      setSubmitError('Could not create the role. Check your connection and try again.');
    } finally {
      setSubmitting(false);
    }
  }, [name, scopes, budget, onCreated]);

  const createAnother = () => {
    setStep(NAME);
    setMaxVisited(NAME);
    setRoleName('');
    setScopes([]);
    setLimits(EMPTY_BUDGET_LIMIT_VALUES);
    setSubmitError(null);
    setCreated(false);
  };

  const body = (() => {
    switch (step) {
      case NAME:
        return (
          <SectionHeading
            icon="user-check"
            title="Name"
            description="The role name as it appears in the OIDC role claim. It cannot be changed after the role is created."
          >
            <FormField id="role-name" label="Role name" required>
              <Input
                type="text"
                placeholder="e.g. ml-engineer"
                value={roleName}
                onChange={(e) => setRoleName(e.target.value)}
                autoFocus
              />
            </FormField>
          </SectionHeading>
        );
      case SCOPES:
        return (
          <SectionHeading
            icon="shield-check"
            title="Scopes"
            description="Choose what members of this role are allowed to do."
          >
            {scopesError ? (
              <Alert variant="error">{scopesError}</Alert>
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
      case BUDGET:
        return (
          <SectionHeading
            icon="dollar-sign"
            title="Budget"
            description="Optional. Turn on the spend limit to cap what this role can spend. With it off, the role has no budget."
          >
            <BudgetLimitFields
              mode="role"
              values={limits}
              onChange={(patch) => setLimits((v) => ({ ...v, ...patch }))}
            />
          </SectionHeading>
        );
      default:
        return (
          <SectionHeading
            icon="rocket"
            title="Review"
            description="Check the details below, then create the role."
          >
            {submitError && <Alert variant="error">{submitError}</Alert>}
            <Wizard.ReviewStep>
              <Wizard.ReviewCard
                icon="user-check"
                title="Name"
                onEdit={() => goTo(NAME)}
                rows={[{ label: 'Role', value: name || 'Not set', mono: name.length > 0 }]}
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
                rows={budgetRows}
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
      icon="user-check"
      title="Create role"
      hint={created ? undefined : hint}
      rail={
        <Wizard.Rail
          label="Create role"
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
          onBack={step > NAME ? () => goTo(step - 1) : undefined}
          onNext={() => goTo(step + 1)}
          nextDisabled={hint !== undefined}
          onSubmit={step === REVIEW ? () => void handleSubmit() : undefined}
          submitLabel="Create role"
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
              <Heading variant="subsection">Role created</Heading>
              <Text size="supporting" color="muted" align="center">
                <Text as="span" size="supporting" weight="heavy" mono>
                  {name}
                </Text>{' '}
                now grants {plural(scopes.length, 'scope', 'scopes')}.
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
          {body}
        </Stack>
      )}
    </Wizard>
  );
}
