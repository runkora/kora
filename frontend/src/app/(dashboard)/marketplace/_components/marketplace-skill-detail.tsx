/** @tier 3 — marketplace-scoped. Read-only presentation of one Skill's details. */

import { Card, CardContent, CardHeader, CodeBlock, Heading, Stack, Text } from '@orbit/terra';
import type { PackageDetail } from '@orbit/shared/api';

interface MarketplaceSkillDetailProps {
  skill: PackageDetail;
}

export function MarketplaceSkillDetail({ skill }: MarketplaceSkillDetailProps) {
  return (
    <Stack gap="relaxed">
      <Stack gap="snug" style={{ minWidth: 0 }}>
        <Heading variant="title">{skill.title_of_package}</Heading>
        <Text size="supporting" color="subtle">
          {skill.name_of_marketplace}
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
              {skill.resources_of_package.length} resource
              {skill.resources_of_package.length === 1 ? '' : 's'}
            </Text>
          </Stack>

          <CodeBlock
            filename={skill.package.name_of_package}
            text={skill.package.body_of_package}
            wrap
          />

          {skill.resources_of_package.map((resource) => (
            <CodeBlock
              key={resource.path_of_resource_in_repo}
              filename={resource.name_of_resource}
              text={resource.body_of_resource}
              wrap
            />
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
                <SourceRow label="Marketplace" value={skill.name_of_marketplace} />
                <SourceRow label="Path" value={skill.package.path_of_package_in_repo} mono />
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
