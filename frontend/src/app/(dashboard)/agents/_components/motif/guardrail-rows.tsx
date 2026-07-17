'use client';

/** @tier 3 — agents-scoped motif: shared between the builder wizard and the detail Configuration tab. */

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
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';

export interface GuardrailValues {
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
  children,
}: {
  on: boolean;
  onToggle: (on: boolean) => void;
  icon: IconName;
  title: string;
  desc: string;
  children?: React.ReactNode;
}) {
  return (
    <Card>
      <CardContent padded compact>
        <Stack direction="horizontal" align="center" gap="cozy">
          <Toggle checked={on} onChange={onToggle} aria-label={title} />
          <Stack direction="horizontal" align="center" gap="cozy" grow disabled={!on}>
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
            {children}
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
          <Text as="span" size="caption" mono color="subtle">
            $
          </Text>
          <Stack maxWidth="form-field-sm">
            <Input
              mono
              compact
              inputMode="decimal"
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
          <Text as="span" size="caption" mono color="subtle">
            $
          </Text>
          <Stack maxWidth="form-field-sm">
            <Input
              mono
              compact
              inputMode="decimal"
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
    </Stack>
  );
}
