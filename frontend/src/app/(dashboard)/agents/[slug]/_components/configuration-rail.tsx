'use client';

/** @tier 3 — agents/[slug]-scoped. The Configuration tab's section rail:
 * numbered nav entries in the builder's anatomy, plus the audit card. */

import { Button, Card, CardContent, Icon, Stack, StepDot, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { formatRelativeTime } from '../../_types';

export interface ConfigSection {
  key: string;
  title: string;
  sub: string;
  icon: IconName;
}

export const CONFIG_SECTIONS: ConfigSection[] = [
  { key: 'identity', title: 'Identity', sub: 'Name & description', icon: 'user' },
  { key: 'behaviour', title: 'Behaviour', sub: 'Prompt & model', icon: 'bot' },
  { key: 'tools', title: 'Tools & access', sub: 'Servers & scopes', icon: 'wrench' },
  { key: 'network', title: 'Network access', sub: 'Connectors & egress', icon: 'network' },
  { key: 'guardrails', title: 'Guardrails', sub: 'Limits & capacity', icon: 'shield' },
  { key: 'danger', title: 'Danger zone', sub: 'Delete agent', icon: 'triangle-alert' },
];

export function ConfigurationRail({
  active,
  onSelect,
  updatedAt,
}: {
  active: string;
  onSelect: (key: string) => void;
  updatedAt: string | null;
}) {
  return (
    <Stack gap="comfortable" minWidth="compact" maxWidth="compact" shrink={false}>
      <Stack gap="tight">
        {CONFIG_SECTIONS.map((section, i) => {
          const isActive = section.key === active;
          return (
            <Button
              key={section.key}
              variant={isActive ? 'secondary' : 'ghost'}
              size="tile"
              fullWidth
              onClick={() => onSelect(section.key)}
              aria-current={isActive ? 'true' : undefined}
            >
              <Stack direction="horizontal" align="center" gap="cozy" padding="snug" grow>
                {isActive ? (
                  <Icon
                    name={section.icon}
                    size="sm"
                    color="accent"
                    container
                    containerVariant="accent"
                    label=""
                  />
                ) : (
                  <StepDot size="lg" state="pending">
                    {i + 1}
                  </StepDot>
                )}
                <Stack gap="tight" align="start" textAlign="start">
                  <Text
                    as="span"
                    size="supporting"
                    weight="heavy"
                    color={isActive ? 'foreground' : 'muted'}
                  >
                    {section.title}
                  </Text>
                  <Text as="span" size="micro" color="subtle">
                    {section.sub}
                  </Text>
                </Stack>
              </Stack>
            </Button>
          );
        })}
      </Stack>

      <Card subtle>
        <CardContent padded compact>
          <Stack gap="tight">
            <Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">
              Audit
            </Text>
            <Text size="micro" color="subtle">
              {updatedAt ? `Last edited ${formatRelativeTime(updatedAt)}. ` : ''}
              Saving re-provisions the agent and recompiles its grants before they apply.
            </Text>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
