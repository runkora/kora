'use client';

/**
 * @tier 3 — triggers-scoped. The section rail of a trigger's Configuration tab.
 * It looks like the New trigger wizard's rail, but sections are free to visit in
 * any order and are never "done", so no step shows a check. Each step is drawn as its
 * icon, and a section holding unsaved changes carries a dot after its title. A `danger`
 * section stands apart below the steps, outside the connector.
 */

import {
  Button,
  Eyebrow,
  Icon,
  NativeSelect,
  Separator,
  Show,
  Stack,
  StatusDot,
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
  /** The section holds changes that are not saved yet. */
  dirty?: boolean;
  /** `danger` draws the section in the error colour, for the actions that cannot be undone. */
  tone?: 'danger';
}

interface ConfigurationRailProps {
  label: string;
  sections: ConfigurationSection[];
  current: string;
  onSelect: (id: string) => void;
}

export function ConfigurationRail({ label, sections, current, onSelect }: ConfigurationRailProps) {
  const steps = sections.filter((s) => s.tone !== 'danger');
  const warnings = sections.filter((s) => s.tone === 'danger');

  const renderStep = (s: ConfigurationSection, connected: boolean) => {
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
        <Stack direction="horizontal" gap="cozy" paddingInline="snug">
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
            <Stack direction="horizontal" gap="snug" align="center">
              <Text
                as="span"
                size="supporting"
                weight="heavy"
                color={danger ? 'status-error' : isCurrent ? 'foreground' : 'subtle'}
              >
                {s.title}
              </Text>
              {s.dirty && <StatusDot status="degraded" size="sm" aria-label="Unsaved changes" />}
            </Stack>
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
            paddingBlock="relaxed"
            paddingInline="comfortable"
            overflowY="auto"
          >
            <Stack paddingInline="snug" paddingBottom="comfortable">
              <Eyebrow>{label}</Eyebrow>
            </Stack>
            <Stack>{steps.map((s, i) => renderStep(s, i < steps.length - 1))}</Stack>
            {warnings.length > 0 && (
              <Stack paddingTop="comfortable">{warnings.map((s) => renderStep(s, false))}</Stack>
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
      </Show>
    </>
  );
}
