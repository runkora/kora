'use client';

/** @tier 3 — agents-scoped. Builder step 4: enforced ceilings + the planned block. */

import {
  Badge,
  Button,
  ButtonGroup,
  Card,
  CardContent,
  Icon,
  Input,
  Separator,
  Stack,
  Text,
  Toggle,
} from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { useAgentBuilderContext } from '../../_context/agent-builder-context';
import { StepHeader } from './step-header';

const PLANNED: { icon: IconName; title: string; desc: string }[] = [
  {
    icon: 'activity',
    title: 'Rate limit',
    desc: 'Cap requests per minute across a run.',
  },
  {
    icon: 'cpu',
    title: 'Max tokens per run',
    desc: 'Context + output ceiling for a single run.',
  },
  {
    icon: 'wrench',
    title: 'Max tool calls per run',
    desc: 'Runaway-loop protection.',
  },
  {
    icon: 'triangle-alert',
    title: 'Halt on repeated failures',
    desc: 'Stop the run after consecutive tool errors.',
  },
  {
    icon: 'eye-off',
    title: 'Redact PII in prompts & logs',
    desc: 'Detected personal data is masked before it reaches the model or the audit trail.',
  },
  {
    icon: 'user-check',
    title: 'Require human approval for write actions',
    desc: 'Destructive tool calls pause for sign-off in the run inspector.',
  },
];

function GuardrailRow({
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

export function StepGuardrails() {
  const { fields, setField } = useAgentBuilderContext();

  return (
    <Stack gap="relaxed">
      <StepHeader
        icon="shield"
        title="Guardrails"
        description="Set the ceilings Orbit enforces at runtime: spend and capacity limits."
      />

      <Stack direction="horizontal" align="baseline" gap="snug">
        <Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">
          Limits
        </Text>
        <Text as="span" size="micro" color="subtle">
          Every limit is optional. Switch on the ones you want Orbit to enforce.
        </Text>
      </Stack>

      <Stack gap="cozy">
        <GuardrailRow
          on={fields.agentBudgetOn}
          onToggle={(on) => setField('agentBudgetOn', on)}
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
                value={fields.agentBudgetUsd}
                onChange={(e) => setField('agentBudgetUsd', e.target.value)}
                aria-label="Agent budget in USD"
              />
            </Stack>
            <ButtonGroup>
              <Button
                variant="outline-muted"
                size="sm"
                selected={fields.agentBudgetCadence === 'weekly'}
                onClick={() => setField('agentBudgetCadence', 'weekly')}
              >
                Weekly
              </Button>
              <Button
                variant="outline-muted"
                size="sm"
                selected={fields.agentBudgetCadence === 'monthly'}
                onClick={() => setField('agentBudgetCadence', 'monthly')}
              >
                Monthly
              </Button>
            </ButtonGroup>
          </Stack>
        </GuardrailRow>

        <GuardrailRow
          on={fields.runBudgetOn}
          onToggle={(on) => setField('runBudgetOn', on)}
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
                value={fields.runBudgetUsd}
                onChange={(e) => setField('runBudgetUsd', e.target.value)}
                aria-label="Run budget in USD"
              />
            </Stack>
          </Stack>
        </GuardrailRow>

        <GuardrailRow
          on={fields.capacityOn}
          onToggle={(on) => setField('capacityOn', on)}
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
                value={fields.capacity}
                onChange={(e) => setField('capacity', e.target.value)}
                aria-label="Max concurrent runs per pod"
              />
            </Stack>
            <Text as="span" size="micro" color="subtle">
              runs / pod
            </Text>
          </Stack>
        </GuardrailRow>
      </Stack>

      {/* ── Planned ────────────────────────────────────────────────────────── */}
      <Card subtle>
        <CardContent flush>
          <Stack direction="horizontal" align="baseline" gap="snug" padding="cozy">
            <Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">
              Planned
            </Text>
            <Text as="span" size="micro" color="subtle">
              On the roadmap, not yet enforced
            </Text>
          </Stack>
          {PLANNED.map((p) => (
            <Stack key={p.title}>
              <Separator span="full" />
              <Stack direction="horizontal" align="center" gap="cozy" padding="cozy" disabled>
                <Icon
                  name={p.icon}
                  size="sm"
                  color="subtle"
                  container
                  containerVariant="surface-subtle"
                  label=""
                />
                <Stack grow gap="tight">
                  <Text as="span" size="supporting" weight="heavy">
                    {p.title}
                  </Text>
                  <Text size="caption" color="muted">
                    {p.desc}
                  </Text>
                </Stack>
                <Badge shape="pill" variant="outline" size="sm">
                  Coming soon
                </Badge>
              </Stack>
            </Stack>
          ))}
        </CardContent>
      </Card>
    </Stack>
  );
}
