'use client';

/**
 * @tier 2 — kora-domain-aware: knows the budget vocabulary (a spend limit per period, a per-run
 * limit, a shared role pool). Laid out like the agent builder's Guardrails rows. Used by: policy
 * (create grant wizard), model-gateway budgets (edit budget modal).
 */

import {
  Button,
  ButtonGroup,
  Card,
  CardContent,
  Icon,
  Input,
  Stack,
  Text,
  Toggle,
} from '@runkora/hades';
import type { IconName } from '@runkora/hades';

/** The limits a budget carries. Structurally a `BudgetAssignment`, without the feature import. */
export interface BudgetLimits {
  limit_usd?: number;
  reset_cadence: string;
  per_run_limit_usd?: number;
  pooled?: boolean;
}

export type BudgetLimitMode = 'grant' | 'role';

export interface BudgetLimitValues {
  spendOn: boolean;
  spendUsd: string;
  cadence: 'weekly' | 'monthly';
  perRunOn: boolean;
  perRunUsd: string;
  /** Role mode only: one shared pot for every member instead of a limit each. */
  pooled: boolean;
}

export const EMPTY_BUDGET_LIMIT_VALUES: BudgetLimitValues = {
  spendOn: false,
  spendUsd: '',
  cadence: 'monthly',
  perRunOn: false,
  perRunUsd: '',
  pooled: false,
};

/** Each row is on when the existing budget has that limit, and off when it does not. */
export function budgetLimitValuesFrom(budget: BudgetLimits | null | undefined): BudgetLimitValues {
  if (!budget) return EMPTY_BUDGET_LIMIT_VALUES;
  return {
    spendOn: !!budget.limit_usd,
    spendUsd: budget.limit_usd ? String(budget.limit_usd) : '',
    cadence: budget.reset_cadence === 'weekly' ? 'weekly' : 'monthly',
    perRunOn: !!budget.per_run_limit_usd,
    perRunUsd: budget.per_run_limit_usd ? String(budget.per_run_limit_usd) : '',
    pooled: budget.pooled ?? false,
  };
}

/** A typed amount: blank counts as 0, anything unreadable as NaN. */
const amount = (text: string): number => (text.trim() === '' ? 0 : Number(text));
const positive = (text: string): boolean => amount(text) > 0;

/** True when a row is on, so a budget is being asked for at all. */
export function budgetLimitsAnyOn(values: BudgetLimitValues, mode: BudgetLimitMode): boolean {
  return values.spendOn || (mode === 'grant' && values.perRunOn);
}

/** Why the values cannot be saved as a budget, or null when they can. */
export function budgetLimitProblem(
  values: BudgetLimitValues,
  mode: BudgetLimitMode,
): string | null {
  if (mode === 'role' && !values.spendOn) return 'Turn on the spend limit.';
  if (!budgetLimitsAnyOn(values, mode)) return 'Turn on a spend limit or a per-run limit.';
  if (values.spendOn && !positive(values.spendUsd)) return 'Enter a spend limit above 0.';
  if (mode === 'grant' && values.perRunOn && !positive(values.perRunUsd)) {
    return 'Enter a per-run limit above 0.';
  }
  return null;
}

/**
 * The budget the values describe, or null when none is on or the values are invalid. The reset
 * cadence is always carried, so a per-run-only budget still has one.
 */
export function budgetFromLimitValues(
  values: BudgetLimitValues,
  mode: BudgetLimitMode,
): BudgetLimits | null {
  if (budgetLimitProblem(values, mode) !== null) return null;
  const budget: BudgetLimits = { reset_cadence: values.cadence };
  if (values.spendOn) budget.limit_usd = amount(values.spendUsd);
  if (mode === 'grant' && values.perRunOn) budget.per_run_limit_usd = amount(values.perRunUsd);
  if (mode === 'role' && values.pooled) budget.pooled = true;
  return budget;
}

interface BudgetLimitRowProps {
  on: boolean;
  onToggle: (on: boolean) => void;
  icon: IconName;
  title: string;
  desc: string;
  /** Dim the row's own content while it is off, leaving its controls rendered in place. */
  dimWhenOff?: boolean;
  error?: string | undefined;
  disabled?: boolean;
  children?: React.ReactNode;
}

// One rounded row: toggle, icon tile, title and description, then the controls. Everything stays
// rendered when the row is off and is only dimmed, so switching a row never moves the layout.
function BudgetLimitRow({
  on,
  onToggle,
  icon,
  title,
  desc,
  dimWhenOff = true,
  error,
  disabled = false,
  children,
}: BudgetLimitRowProps) {
  return (
    <Card>
      <CardContent padded compact>
        <Stack direction="horizontal" align="center" gap="cozy">
          <Toggle checked={on} onChange={onToggle} aria-label={title} disabled={disabled} />
          <Stack grow gap="snug" disabled={dimWhenOff && !on}>
            <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="cozy">
              <Stack direction="horizontal" align="center" gap="cozy" grow>
                <Icon
                  name={icon}
                  size="sm"
                  color={on ? 'accent' : 'subtle'}
                  container
                  containerVariant={on ? 'accent' : 'surface-subtle'}
                  label=""
                />
                <Stack grow gap="tight">
                  <Text as="span" size="supporting" weight="heavy">
                    {title}
                  </Text>
                  <Text size="caption" color="muted">
                    {desc}
                  </Text>
                </Stack>
              </Stack>
              {children}
            </Stack>
            {error && (
              <Text size="caption" color="status-error" role="alert">
                {error}
              </Text>
            )}
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}

export interface BudgetLimitFieldsProps {
  /** "grant" has a spend limit and a per-run limit; "role" has a spend limit and a shared pool. */
  mode: BudgetLimitMode;
  values: BudgetLimitValues;
  onChange: (patch: Partial<BudgetLimitValues>) => void;
  disabled?: boolean;
}

/**
 * BudgetLimitFields
 *
 * The limits of a budget as switchable rows. A row that is switched on needs an amount above 0;
 * the row says so when the typed amount is not. Pair it with `budgetLimitValuesFrom` to prefill,
 * `budgetLimitProblem` for the message that explains why saving is blocked, and
 * `budgetFromLimitValues` for the payload.
 */
export function BudgetLimitFields({
  mode,
  values,
  onChange,
  disabled = false,
}: BudgetLimitFieldsProps) {
  const spendError =
    values.spendOn && values.spendUsd.trim() !== '' && !positive(values.spendUsd)
      ? 'Enter an amount above 0.'
      : undefined;
  const perRunError =
    values.perRunOn && values.perRunUsd.trim() !== '' && !positive(values.perRunUsd)
      ? 'Enter an amount above 0.'
      : undefined;

  return (
    <Stack gap="cozy">
      <BudgetLimitRow
        on={values.spendOn}
        onToggle={(on) => onChange({ spendOn: on })}
        icon="dollar-sign"
        title="Spend limit"
        desc="Total spend cap per period"
        error={spendError}
        disabled={disabled}
      >
        <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
          <Stack maxWidth="form-field-sm">
            <Input
              disabled={disabled}
              mono
              compact
              inputMode="decimal"
              placeholder="e.g. 50"
              startAdornment="$"
              hasError={Boolean(spendError)}
              value={values.spendUsd}
              onChange={(e) => onChange({ spendUsd: e.target.value })}
              aria-label="Spend limit in USD"
            />
          </Stack>
          <ButtonGroup role="group" aria-label="Reset window">
            <Button
              disabled={disabled}
              variant="outline-muted"
              size="sm"
              selected={values.cadence === 'weekly'}
              onClick={() => onChange({ cadence: 'weekly' })}
            >
              Weekly
            </Button>
            <Button
              disabled={disabled}
              variant="outline-muted"
              size="sm"
              selected={values.cadence === 'monthly'}
              onClick={() => onChange({ cadence: 'monthly' })}
            >
              Monthly
            </Button>
          </ButtonGroup>
        </Stack>
      </BudgetLimitRow>

      {mode === 'grant' && (
        <BudgetLimitRow
          on={values.perRunOn}
          onToggle={(on) => onChange({ perRunOn: on })}
          icon="dollar-sign"
          title="Per-run limit"
          desc="Caps a single agent run started on this person's behalf"
          error={perRunError}
          disabled={disabled}
        >
          <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
            <Stack maxWidth="form-field-sm">
              <Input
                disabled={disabled}
                mono
                compact
                inputMode="decimal"
                placeholder="e.g. 5"
                startAdornment="$"
                hasError={Boolean(perRunError)}
                value={values.perRunUsd}
                onChange={(e) => onChange({ perRunUsd: e.target.value })}
                aria-label="Per-run limit in USD"
              />
            </Stack>
            <Text as="span" size="micro" color="subtle">
              / run
            </Text>
          </Stack>
        </BudgetLimitRow>
      )}

      {mode === 'role' && (
        <BudgetLimitRow
          on={values.pooled}
          onToggle={(on) => onChange({ pooled: on })}
          icon="users"
          title="Shared pool"
          desc="One pot for every member's spend, instead of a limit each"
          dimWhenOff={false}
          disabled={disabled}
        />
      )}
    </Stack>
  );
}
