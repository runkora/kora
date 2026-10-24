'use client';

/** @tier 3 — policy-scoped. Creates a grant for a person: user, scopes, an optional budget, then
 * review. It offers no other principal type: agent, trigger and token grants are written by the
 * agent, trigger or token they belong to, and the policy-service refuses them from anyone but a
 * SYSTEM caller. */

import { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Icon,
  SectionHeading,
  Stack,
  Text,
  Wizard,
  useWizardSteps,
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
import { fmtUsd, plural } from '@runkora/utils';
import type { PATAttributionEntry } from '../../model-gateway/_services/tokens-server';
import { createGrantAction } from '../_actions/grants';
import { UserPicker } from './user-picker';

interface CreateGrantWizardProps {
  /** Every scope the grant can carry. */
  availableScopes: string[];
  /** The vocabulary entries behind `availableScopes`; the picker groups by them and shows descriptions. */
  scopeOptions?: ScopeOption[];
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

export function CreateGrantWizard({
  availableScopes,
  scopeOptions,
  onClose,
  onCreated,
}: CreateGrantWizardProps) {
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [created, setCreated] = useState(false);

  const [userId, setUserId] = useState('');
  const [user, setUser] = useState<PATAttributionEntry | null>(null);
  const [scopes, setScopes] = useState<string[]>([]);
  const [limits, setLimits] = useState<BudgetLimitValues>(EMPTY_BUDGET_LIMIT_VALUES);

  // Both rows off means no budget. A row that is on needs an amount above 0.
  const budgetAsked = budgetLimitsAnyOn(limits, 'grant');
  const budget = useMemo(() => budgetFromLimitValues(limits, 'grant'), [limits]);
  const budgetProblem = budgetAsked ? budgetLimitProblem(limits, 'grant') : null;

  const budgetRows = [
    ...(budget?.limit_usd
      ? [
          {
            label: 'Spend limit',
            value: `${fmtUsd(budget.limit_usd)} per ${budget.reset_cadence === 'weekly' ? 'week' : 'month'}`,
          },
        ]
      : []),
    ...(budget?.per_run_limit_usd
      ? [{ label: 'Per-run limit', value: `${fmtUsd(budget.per_run_limit_usd)} per run` }]
      : []),
  ];
  if (budgetRows.length === 0) budgetRows.push({ label: 'Budget', value: 'No budget' });

  const userName = user ? user.display_name || user.email : userId;

  const hints: (string | undefined)[] = [
    userId ? undefined : 'Choose a user before you continue.',
    scopes.length === 0 ? 'Choose at least one scope before you continue.' : undefined,
    budgetProblem ?? undefined,
    undefined,
  ];
  const steps = useWizardSteps({
    stepCount: STEPS.length,
    hints,
    locked: submitting,
    reviewStep: REVIEW,
    onNavigate: () => setSubmitError(null),
  });

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
    steps.reset();
    setUserId('');
    setUser(null);
    setScopes([]);
    setLimits(EMPTY_BUDGET_LIMIT_VALUES);
    setSubmitError(null);
    setCreated(false);
  };

  const body = (() => {
    switch (steps.step) {
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
            <ScopePicker
              options={scopeOptions}
              scopes={availableScopes}
              selected={scopes}
              onChange={setScopes}
            />
          </SectionHeading>
        );
      case BUDGET:
        return (
          <SectionHeading
            icon="dollar-sign"
            title="Budget"
            description="Optional. Turn on a limit to cap what this grant can spend. With none on, the grant has no budget."
          >
            <BudgetLimitFields
              mode="grant"
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
            description="Check the details below, then create the grant."
          >
            {submitError && <Alert variant="error">{submitError}</Alert>}
            <Wizard.ReviewStep>
              <Wizard.ReviewCard
                icon="user"
                title="User"
                onEdit={() => steps.goTo(USER)}
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
                onEdit={() => steps.goTo(SCOPES)}
                rows={[{ label: 'Selected', value: plural(scopes.length, 'scope', 'scopes') }]}
              >
                <Stack paddingBlock="snug">
                  <ScopeChipList scopes={scopes} max={8} />
                </Stack>
              </Wizard.ReviewCard>
              <Wizard.ReviewCard
                icon="dollar-sign"
                title="Budget"
                onEdit={() => steps.goTo(BUDGET)}
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
      icon="shield-check"
      title="Create grant"
      hint={created ? undefined : steps.hint}
      rail={
        <Wizard.Rail
          label="Create grant"
          steps={STEPS}
          current={steps.step}
          maxVisited={steps.maxVisited}
          onGoTo={steps.goTo}
        />
      }
      footer={
        <Wizard.Footer
          step={steps.step}
          totalSteps={STEPS.length}
          onBack={steps.isFirst ? undefined : steps.back}
          onNext={steps.next}
          nextDisabled={steps.nextDisabled}
          onSubmit={steps.isLast ? () => void handleSubmit() : undefined}
          submitLabel="Create grant"
          pending={submitting}
        />
      }
    >
      {created ? (
        <Wizard.Success
          title="Grant created"
          description={
            <>
              <Text as="span" size="supporting" weight="heavy">
                {userName}
              </Text>{' '}
              now has {plural(scopes.length, 'scope', 'scopes')}.
            </>
          }
          actions={
            <>
              <Button variant="secondary" onClick={onClose}>
                Done
              </Button>
              <Button onClick={createAnother}>
                Create another
                <Icon name="plus" />
              </Button>
            </>
          }
        />
      ) : (
        <Stack grow overflowY="auto" padding="loose" gap="relaxed">
          {body}
        </Stack>
      )}
    </Wizard>
  );
}
