'use client';

/** @tier 3 — agents/[slug]-scoped. The Configuration tab's section rail:
 * numbered nav entries in the builder's anatomy, plus the audit card. */

import { Card, CardContent, Eyebrow, SectionNav, Stack, Text } from '@runkora/hades';
import type { SectionNavSection } from '@runkora/hades';
import { formatRelativeTime } from '../../_types';

export type ConfigSection = SectionNavSection;

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
  canEdit,
}: {
  active: string;
  onSelect: (key: string) => void;
  updatedAt: string | null;
  /** Without edit access the Danger zone, which only deletes, is left out. */
  canEdit: boolean;
}) {
  const sections = canEdit ? CONFIG_SECTIONS : CONFIG_SECTIONS.filter((s) => s.key !== 'danger');
  return (
    <Stack gap="comfortable" minWidth="compact" maxWidth="compact" shrink={false}>
      <SectionNav sections={sections} active={active} onSelect={onSelect} />

      <Card subtle>
        <CardContent padded compact>
          <Stack gap="tight">
            <Eyebrow>Audit</Eyebrow>
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
