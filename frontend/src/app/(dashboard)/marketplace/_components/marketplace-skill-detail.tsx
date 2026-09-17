/** @tier 3 — marketplace-scoped. Read-only presentation of one Skill's details. */

import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  SidebarLayout,
  SidebarLayoutMain,
  SidebarLayoutSidebar,
  Stack,
  Text,
} from '@orbit/terra';
import type { PackageDetail } from '@orbit/shared/api';

interface SkillDetailProps {
  skill: PackageDetail;
}

export function SkillDetail({ skill }: SkillDetailProps) {
  const [org, repo] = skill.slug.split('/');

  return (
    <Stack gap="relaxed">
      <Stack direction="horizontal" align="start" gap="comfortable">
        <Stack grow gap="none" style={{ minWidth: 0 }}>
          <Text as="span" weight="heavy" style={{ fontSize: 26, letterSpacing: '-0.02em' }}>
            {skill.title_of_package}
          </Text>
          <Stack direction="horizontal" align="center" wrap style={{ gap: 10, marginTop: 6 }}>
            <Text as="span" mono weight="strong" color="muted" style={{ fontSize: 13 }}>
              {skill.slug}
            </Text>
            <Text as="span" style={{ color: 'var(--color-border)' }}>
              ·
            </Text>
            <Text as="span" color="subtle" style={{ fontSize: 13 }}>
              {skill.name_of_marketplace}
            </Text>
          </Stack>
          <Text
            size="supporting"
            color="muted"
            style={{ margin: '10px 0 0', lineHeight: 1.55, maxWidth: 620 }}
          >
            {skill.description}
          </Text>
        </Stack>
      </Stack>

      <SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align="start">
        <SidebarLayoutMain>
          <Stack gap="comfortable" style={{ minWidth: 0 }}>
            <Stack direction="horizontal" align="baseline" justify="between">
              <Text as="span" weight="strong" style={{ fontSize: 17 }}>
                Contents
              </Text>
              <Text as="span" size="caption" color="subtle">
                {skill.resources_of_package.length} resource
                {skill.resources_of_package.length === 1 ? '' : 's'}
              </Text>
            </Stack>

            <FileCard name={skill.package.name_of_package} body={skill.package.body_of_package} />

            {skill.resources_of_package.map((resource) => (
              <FileCard
                key={resource.path_of_resource_in_repo}
                name={resource.name_of_resource}
                body={resource.body_of_resource}
              />
            ))}
          </Stack>
        </SidebarLayoutMain>

        <SidebarLayoutSidebar>
          <Stack gap="comfortable" style={{ position: 'sticky', top: 0 }}>
            <Card column>
              <CardHeader>
                <CardTitle>Source</CardTitle>
              </CardHeader>
              <CardContent>
                <Stack gap="cozy">
                  <SourceRow label="Marketplace" value={skill.name_of_marketplace} />
                  <SourceRow label="Organisation" value={org} mono />
                  <SourceRow label="Repository" value={repo} mono />
                  <SourceRow label="Path" value={skill.package.path_of_package_in_repo} mono />
                </Stack>
              </CardContent>
            </Card>
          </Stack>
        </SidebarLayoutSidebar>
      </SidebarLayout>
    </Stack>
  );
}

function FileCard({ name, body }: { name: string; body: string }) {
  return (
    <Card column>
      <CardHeader>
        <CardTitle>{name}</CardTitle>
      </CardHeader>
      <CardContent>
        <Text mono size="supporting" style={{ whiteSpace: 'pre-wrap', lineHeight: 1.6 }}>
          {body}
        </Text>
      </CardContent>
    </Card>
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
