'use client';

/** @tier 3 — marketplace-scoped. Skill detail view: description, SKILL.md content, and resource documents. */

import Link from 'next/link';
import { Heading, Stack, Text, Code } from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { Skill, SkillFile } from '@orbit/lib/marketplace';

interface SkillDetailPageProps {
  marketplace: string;
  skillName: string;
  skill: Skill | null;
  error: string | null;
}

function DocumentCard({ file }: { file: SkillFile }) {
  return (
    <Stack gap="tight" padding="comfortable">
      <Text weight="emphasis" mono size="supporting">
        {file.name}
      </Text>
      <Text size="caption" color="muted" mono>
        {file.path}
      </Text>
      <Code>{file.content}</Code>
    </Stack>
  );
}

export function SkillDetailPage({ marketplace, skillName, skill, error }: SkillDetailPageProps) {
  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Stack direction="horizontal" align="center" gap="snug">
          <Link href={APP_ROUTES.marketplace.list} style={{ textDecoration: 'none' }}>
            <Text size="supporting" color="muted">
              Marketplace
            </Text>
          </Link>
          <Text size="supporting" color="muted">
            /
          </Text>
          <Link href={APP_ROUTES.marketplace.skills(marketplace)} style={{ textDecoration: 'none' }}>
            <Text size="supporting" color="muted">
              {marketplace}
            </Text>
          </Link>
          <Text size="supporting" color="muted">
            /
          </Text>
          <Text size="supporting">{skillName}</Text>
        </Stack>

        {error ? (
          <Text size="supporting" color="muted">
            {error}
          </Text>
        ) : skill ? (
          <>
            <Heading variant="title">{skill.name}</Heading>
            <Text size="supporting" color="muted">
              {skill.description}
            </Text>

            <Stack gap="tight">
              <Heading variant="section">Skill</Heading>
              <DocumentCard file={skill.skill} />
            </Stack>

            {skill.documents.length > 0 && (
              <Stack gap="tight">
                <Heading variant="section">
                  Resources ({skill.documents.length})
                </Heading>
                {skill.documents.map((doc) => (
                  <DocumentCard key={doc.path} file={doc} />
                ))}
              </Stack>
            )}
          </>
        ) : null}
      </Stack>
    </Stack>
  );
}
