'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

import {
  Button,
  ButtonGroup,
  Card,
  CardContent,
  Icon,
  Input,
  Show,
  Stack,
  Text,
  Toggle,
} from '@runkora/hades';
import type { IconName } from '@runkora/hades';
import {
  LOOP_GUARDRAIL_DEFAULTS,
  loopGuardrailErrors,
  type LoopGuardrailValues,
} from '../../_types';

export interface GuardrailValues extends LoopGuardrailValues {
  agentBudgetOn: boolean;
  agentBudgetUsd: string;
  agentBudgetCadence: 'weekly' | 'monthly';
  runBudgetOn: boolean;
  runBudgetUsd: string;
  capacityOn: boolean;
  capacity: string;
}

export function GuardrailRow({
  on,
  onToggle,
  icon,
  title,
  desc,
  error,
  disabled = false,
  children,
}: {
  on: boolean;
  onToggle: (on: boolean) => void;
  icon: IconName;
  title: string;
  desc: string;
  error?: string;
  /** Read-only, for callers who may not change the agent. */
  disabled?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <Card>
      <CardContent padded compact>
        <Stack direction="horizontal" align="center" gap="cozy">
          <Toggle checked={on} onChange={onToggle} aria-label={title} disabled={disabled} />
          <Stack grow gap="snug" disabled={!on}>
            <Stack direction="horizontal" align="center" gap="cozy">
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
              <Show above="lg">{children}</Show>
            </Stack>
            {children && <Show below="lg">{children}</Show>}
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

export function GuardrailsFields({
  values,
  onChange,
  disabled = false,
}: {
  values: GuardrailValues;
  onChange: <K extends keyof GuardrailValues>(key: K, value: GuardrailValues[K]) => void;
  /** Read-only, for callers who may not change the agent. */
  disabled?: boolean;
}) {
  const loopErrors = loopGuardrailErrors(values);
  return (
    <Stack gap="cozy">
      <GuardrailRow
        disabled={disabled}
        on={values.agentBudgetOn}
        onToggle={(on) => onChange('agentBudgetOn', on)}
        icon="dollar-sign"
        title="Agent budget"
        desc="Total spend cap per period"
      >
        <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
          <Stack maxWidth="form-field-sm">
            <Input
              disabled={disabled}
              mono
              compact
              inputMode="decimal"
              startAdornment="$"
              value={values.agentBudgetUsd}
              onChange={(e) => onChange('agentBudgetUsd', e.target.value)}
              aria-label="Agent budget in USD"
            />
          </Stack>
          <ButtonGroup>
            <Button
              disabled={disabled}
              variant="outline-muted"
              size="sm"
              selected={values.agentBudgetCadence === 'weekly'}
              onClick={() => onChange('agentBudgetCadence', 'weekly')}
            >
              Weekly
            </Button>
            <Button
              disabled={disabled}
              variant="outline-muted"
              size="sm"
              selected={values.agentBudgetCadence === 'monthly'}
              onClick={() => onChange('agentBudgetCadence', 'monthly')}
            >
              Monthly
            </Button>
          </ButtonGroup>
        </Stack>
      </GuardrailRow>

      <GuardrailRow
        disabled={disabled}
        on={values.runBudgetOn}
        onToggle={(on) => onChange('runBudgetOn', on)}
        icon="dollar-sign"
        title="Run budget"
        desc="Max spend on a single run"
      >
        <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
          <Stack maxWidth="form-field-sm">
            <Input
              disabled={disabled}
              mono
              compact
              inputMode="decimal"
              startAdornment="$"
              value={values.runBudgetUsd}
              onChange={(e) => onChange('runBudgetUsd', e.target.value)}
              aria-label="Run budget in USD"
            />
          </Stack>
        </Stack>
      </GuardrailRow>

      <GuardrailRow
        disabled={disabled}
        on={values.capacityOn}
        onToggle={(on) => onChange('capacityOn', on)}
        icon="layers"
        title="Capacity"
        desc="Max concurrent runs per pod (service default when off)"
      >
        <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
          <Stack maxWidth="form-field-sm">
            <Input
              disabled={disabled}
              mono
              compact
              inputMode="numeric"
              value={values.capacity}
              onChange={(e) => onChange('capacity', e.target.value)}
              aria-label="Max concurrent runs per pod"
            />
          </Stack>
          <Text as="span" size="micro" color="subtle">
            runs / pod
          </Text>
        </Stack>
      </GuardrailRow>

      <LoopThresholdRow
        disabled={disabled}
        icon="refresh-cw"
        title="Repeated calls"
        desc="Same tool call, same input and result, in a row (platform default when off)"
        toggle="repeatedCallOn"
        warn="repeatedCallWarn"
        pause="repeatedCallPause"
        error={loopErrors.repeatedCall}
        values={values}
        onChange={onChange}
      />
      <LoopThresholdRow
        disabled={disabled}
        icon="clock"
        title="Wait loops"
        desc="Waits re-armed with no progress in between (platform default when off)"
        toggle="waitRearmOn"
        warn="waitRearmWarn"
        pause="waitRearmPause"
        error={loopErrors.waitRearm}
        values={values}
        onChange={onChange}
      />
    </Stack>
  );
}

/**
 * One loop-guard threshold pair: kora warns the agent at the first number and
 * pauses the run at the second. The guard always runs; switched off, the agent
 * type takes the platform defaults, which the disabled inputs show.
 */
function LoopThresholdRow({
  icon,
  title,
  desc,
  toggle,
  warn,
  pause,
  error,
  values,
  onChange,
  disabled,
}: {
  icon: IconName;
  title: string;
  desc: string;
  toggle: 'repeatedCallOn' | 'waitRearmOn';
  warn: 'repeatedCallWarn' | 'waitRearmWarn';
  pause: 'repeatedCallPause' | 'waitRearmPause';
  error?: string;
  values: GuardrailValues;
  onChange: <K extends keyof GuardrailValues>(key: K, value: GuardrailValues[K]) => void;
  disabled: boolean;
}) {
  const setOn = (on: boolean) => {
    onChange(toggle, on);
    if (!on) {
      onChange(warn, String(LOOP_GUARDRAIL_DEFAULTS[warn]));
      onChange(pause, String(LOOP_GUARDRAIL_DEFAULTS[pause]));
    }
  };
  const input = (key: typeof warn | typeof pause, label: string) => (
    <Stack direction="horizontal" align="center" justify="end" gap="snug">
      <Text as="span" size="micro" color="subtle">
        {label}
      </Text>
      <Stack maxWidth="form-field-sm">
        <Input
          disabled={disabled}
          mono
          compact
          inputMode="numeric"
          hasError={Boolean(error)}
          value={values[key]}
          onChange={(e) => onChange(key, e.target.value)}
          aria-label={`${title}: ${label.toLowerCase()}`}
        />
      </Stack>
    </Stack>
  );
  return (
    <GuardrailRow
      disabled={disabled}
      on={values[toggle]}
      onToggle={setOn}
      icon={icon}
      title={title}
      desc={desc}
      error={error}
    >
      {/* Sized to its content, so the inputs start where the other rows' do when they wrap below. */}
      <Stack direction="horizontal" shrink={false}>
        <Stack gap="snug">
          {input(warn, 'Warn after')}
          {input(pause, 'Pause after')}
        </Stack>
      </Stack>
    </GuardrailRow>
  );
}
