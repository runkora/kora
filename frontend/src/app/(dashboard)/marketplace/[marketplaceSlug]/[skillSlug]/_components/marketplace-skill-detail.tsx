/** @tier 3 — marketplace/[marketplaceSlug]/[skillSlug]-scoped. Read-only presentation of one Skill's details. */

import { Card, CardContent, CardHeader, CodeBlock, Heading, Stack, Text } from '@orbit/hades';
import type { Skill } from '@orbit/shared/api';

interface MarketplaceSkillDetailProps {
  skill: Skill;
  /** The display name of the Marketplace. The skills-service Skill has no Marketplace field. */
  marketplaceName: string;
}

export function MarketplaceSkillDetail({ skill, marketplaceName }: MarketplaceSkillDetailProps) {
  return (
    <Stack gap="relaxed">
      <Stack gap="snug" style={{ minWidth: 0 }}>
        <Heading variant="title">{skill.name}</Heading>
        <Text size="supporting" color="subtle">
          {marketplaceName}
        </Text>
        <Stack maxWidth="prose">
          <Text size="supporting" color="muted">
            {skill.description}
          </Text>
        </Stack>
      </Stack>

      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="relaxed" align="start">
        <Stack gap="comfortable" grow minWidth="none" style={{ width: '100%' }}>
          <Stack direction="horizontal" align="baseline" justify="between">
            <Heading variant="section">Contents</Heading>
            <Text as="span" size="caption" color="subtle">
              {skill.documents.length} resource
              {skill.documents.length === 1 ? '' : 's'}
            </Text>
          </Stack>

          <CodeBlock filename={skill.skill.name} text={skill.skill.content} wrap />

          {skill.documents.map((resource) => (
            <CodeBlock key={resource.path} filename={resource.name} text={resource.content} wrap />
          ))}
        </Stack>
        <Stack
          gap="comfortable"
          shrink
          minWidth="none"
          maxWidth={{ base: 'full', lg: 'detail-panel' }}
          style={{ width: '100%', position: 'sticky', top: 0 }}
        >
          <Card column>
            <CardHeader title="Source" />
            <CardContent>
              <Stack gap="cozy">
                <SourceRow label="Marketplace" value={marketplaceName} />
                <SourceRow label="Path" value={skill.skill.path} mono />
              </Stack>
            </CardContent>
          </Card>
        </Stack>
      </Stack>
    </Stack>
  );
}

function SourceRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <Stack gap="none" style={{ minWidth: 0 }}>
      <Text as="span" size="caption" color="subtle">
        {label}
      </Text>
      <Text as="span" size="supporting" mono={mono} truncate>
        {value}
      </Text>
    </Stack>
  );
}
