'use client';

/**
 * @tier 3 — agents/[slug]-scoped. The section rail of the Configure modal, drawn
 * like the trigger modal's: sections are free to visit in any order and are never
 * "done", so each step is only its icon, with no number or check. A `danger`
 * section stands apart below the steps, outside the connector, in the error colour.
 */

import {
  Button,
  Eyebrow,
  Icon,
  NativeSelect,
  Separator,
  Show,
  Stack,
  StepConnector,
  StepDot,
  Text,
} from '@runkora/hades';
import type { IconName } from '@runkora/hades';

export interface ConfigurationSection {
  id: string;
  title: string;
  /** A few words on what the section controls, shown under the title. */
  sub: string;
  /** Drawn inside the step's circle. */
  icon: IconName;
  /** `danger` draws the section in the error colour, for the actions that cannot be undone. */
  tone?: 'danger';
}

export const CONFIG_SECTIONS: ConfigurationSection[] = [
  { id: 'identity', title: 'Identity', sub: 'Name & description', icon: 'user' },
  { id: 'behaviour', title: 'Behaviour', sub: 'Prompt & model', icon: 'bot' },
  { id: 'tools', title: 'Tools & access', sub: 'Servers & scopes', icon: 'wrench' },
  { id: 'network', title: 'Network access', sub: 'Connectors & egress', icon: 'network' },
  { id: 'guardrails', title: 'Guardrails', sub: 'Limits & capacity', icon: 'shield' },
  {
    id: 'danger',
    title: 'Danger zone',
    sub: 'Delete agent',
    icon: 'triangle-alert',
    tone: 'danger',
  },
];

interface ConfigurationRailProps {
  label: string;
  sections: ConfigurationSection[];
  current: string;
  onSelect: (id: string) => void;
}

export function ConfigurationRail({ label, sections, current, onSelect }: ConfigurationRailProps) {
  const steps = sections.filter((s) => s.tone !== 'danger');
  const warnings = sections.filter((s) => s.tone === 'danger');

  // Steps sit flush and each pads itself, so the hover highlight clears the icon above
  // and the connector below. The danger zone has no connector on either side, so it pads more.
  const renderStep = (s: ConfigurationSection, connected: boolean, isolated = false) => {
    const isCurrent = s.id === current;
    const danger = s.tone === 'danger';
    return (
      <Button
        key={s.id}
        variant="ghost"
        size="tile"
        fullWidth
        onClick={() => onSelect(s.id)}
        aria-current={isCurrent ? 'step' : undefined}
      >
        <Stack
          direction="horizontal"
          gap="cozy"
          paddingInline="snug"
          paddingBlock={isolated ? 'snug' : 'tight'}
        >
          <Stack align="center" gap="none" shrink={false}>
            <StepDot size="lg" state={isCurrent ? 'current' : 'pending'}>
              <Icon
                name={s.icon}
                size="xs"
                color={danger && !isCurrent ? 'status-error' : undefined}
              />
            </StepDot>
            {connected && <StepConnector complete={false} />}
          </Stack>
          <Stack
            gap="tight"
            paddingBottom={connected ? 'relaxed' : 'none'}
            paddingTop="tight"
            textAlign="start"
          >
            <Text
              as="span"
              size="supporting"
              weight="heavy"
              color={danger ? 'status-error' : isCurrent ? 'foreground' : 'subtle'}
            >
              {s.title}
            </Text>
            <Text as="span" size="micro" color={danger ? 'status-error' : 'subtle'}>
              {s.sub}
            </Text>
          </Stack>
        </Stack>
      </Button>
    );
  };

  return (
    <>
      <Show above="lg">
        <Stack direction="horizontal" height="full" gap="none" shrink={false}>
          <Stack
            data-slot="configuration-rail"
            height="full"
            minWidth="compact"
            maxWidth="compact"
            shrink={false}
            paddingTop="relaxed"
            paddingBottom="relaxed"
            paddingInline="comfortable"
            overflowY="auto"
          >
            <Stack paddingInline="snug" paddingBottom="cozy">
              <Eyebrow>{label}</Eyebrow>
            </Stack>
            <Stack gap="none">{steps.map((s, i) => renderStep(s, i < steps.length - 1))}</Stack>
            {warnings.length > 0 && (
              <Stack paddingTop="tight">{warnings.map((s) => renderStep(s, false, true))}</Stack>
            )}
          </Stack>
          <Separator orientation="vertical" />
        </Stack>
      </Show>
      {/* Below lg the rail has no room beside the content, so it folds into a select. */}
      <Show below="lg">
        <Stack paddingBlock="snug" paddingInline="comfortable">
          <NativeSelect
            aria-label="Configuration section"
            value={current}
            onChange={(e) => onSelect(e.target.value)}
          >
            {sections.map((s) => (
              <option key={s.id} value={s.id}>
                {s.title}
              </option>
            ))}
          </NativeSelect>
        </Stack>
        <Separator />
      </Show>
    </>
  );
}
