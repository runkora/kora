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
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { loopGuardrailErrors, type LoopGuardrailValues } from '../../_types';

export interface GuardrailValues extends LoopGuardrailValues {
  agentBudgetOn: boolean;
  agentBudgetUsd: string;
  agentBudgetCadence: 'weekly' | 'monthly';
  runBudgetOn: boolean;
  runBudgetUsd: string;
  capacityOn: boolean;
  capacity: string;
}

/** A limit row. Without onToggle the limit is always enforced and shows no switch. */
export function GuardrailRow({
  on = true,
  onToggle,
  icon,
  title,
  desc,
  error,
  children,
}: {
  on?: boolean;
  onToggle?: (on: boolean) => void;
  icon: IconName;
  title: string;
  desc: string;
  error?: string;
  children?: React.ReactNode;
}) {
  return (
    <Card>
      <CardContent padded compact>
        <Stack direction="horizontal" align="center" gap="cozy">
          {onToggle && <Toggle checked={on} onChange={onToggle} aria-label={title} />}
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
}: {
  values: GuardrailValues;
  onChange: <K extends keyof GuardrailValues>(key: K, value: GuardrailValues[K]) => void;
}) {
  const loopErrors = loopGuardrailErrors(values);
  return (
    <Stack gap="cozy">
      <GuardrailRow
        on={values.agentBudgetOn}
        onToggle={(on) => onChange('agentBudgetOn', on)}
        icon="dollar-sign"
        title="Agent budget"
        desc="Total spend cap per period"
      >
        <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
          <Stack maxWidth="form-field-sm">
            <Input
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
              variant="outline-muted"
              size="sm"
              selected={values.agentBudgetCadence === 'weekly'}
              onClick={() => onChange('agentBudgetCadence', 'weekly')}
            >
              Weekly
            </Button>
            <Button
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
        on={values.runBudgetOn}
        onToggle={(on) => onChange('runBudgetOn', on)}
        icon="dollar-sign"
        title="Run budget"
        desc="Max spend on a single run"
      >
        <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
          <Stack maxWidth="form-field-sm">
            <Input
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
        on={values.capacityOn}
        onToggle={(on) => onChange('capacityOn', on)}
        icon="layers"
        title="Capacity"
        desc="Max concurrent runs per pod (service default when off)"
      >
        <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
          <Stack maxWidth="form-field-sm">
            <Input
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
        icon="refresh-cw"
        title="Repeated calls"
        desc="The same tool call with the same input and the same result, in a row"
        warn="repeatedCallWarn"
        pause="repeatedCallPause"
        error={loopErrors.repeatedCall}
        values={values}
        onChange={onChange}
      />
      <LoopThresholdRow
        icon="clock"
        title="Wait loops"
        desc="Waits re-armed (monitors, scheduled wake-ups, sleeps) with no progress in between"
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
 * One loop-guard threshold pair. Orbit warns the agent at the first number and
 * pauses the run for the user at the second; both are always enforced.
 */
function LoopThresholdRow({
  icon,
  title,
  desc,
  warn,
  pause,
  error,
  values,
  onChange,
}: {
  icon: IconName;
  title: string;
  desc: string;
  warn: keyof LoopGuardrailValues;
  pause: keyof LoopGuardrailValues;
  error?: string;
  values: GuardrailValues;
  onChange: <K extends keyof GuardrailValues>(key: K, value: GuardrailValues[K]) => void;
}) {
  const input = (key: keyof LoopGuardrailValues, label: string) => (
    <Stack direction="horizontal" align="center" gap="snug">
      <Text as="span" size="micro" color="subtle">
        {label}
      </Text>
      <Stack maxWidth="form-field-sm">
        <Input
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
    <GuardrailRow icon={icon} title={title} desc={desc} error={error}>
      <Stack direction="horizontal" align="center" gap="cozy" shrink={false}>
        {input(warn, 'Warn after')}
        {input(pause, 'Pause after')}
      </Stack>
    </GuardrailRow>
  );
}
