'use client';

/** @tier 3 — marketplace-scoped. Read-only list of skills in a marketplace. */

import { useState } from 'react';
import Link from 'next/link';
import { Heading, Input, Stack, Text } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { SkillSummary } from '@orbit/lib/marketplace';

interface SkillsPageProps {
  marketplace: string;
  skills: SkillSummary[];
  initialSearch: string;
  error: string | null;
}

export function SkillsPage({ marketplace, skills, initialSearch, error }: SkillsPageProps) {
  const [search, setSearch] = useState(initialSearch);

  const filtered = search
    ? skills.filter((s) => s.name.toLowerCase().includes(search.toLowerCase()))
    : skills;

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
          <Text size="supporting">{marketplace}</Text>
        </Stack>
        <Heading variant="title">{marketplace}</Heading>
        <Text size="supporting" color="muted">
          {skills.length} skill{skills.length !== 1 ? 's' : ''} available
        </Text>
      </Stack>

      <Input
        placeholder="Search skills…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />

      {error ? (
        <Text size="supporting" color="muted">
          {error}
        </Text>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon="box"
          title="No skills found"
          description={search ? 'No skills match your search.' : 'This marketplace has no skills yet.'}
        />
      ) : (
        <Stack gap="snug">
          {filtered.map((skill) => {
            const skillSlug = skill.name
              .toLowerCase()
              .replace(/\s+/g, '-')
              .replace(/[^a-z0-9-]/g, '');
            return (
              <Link
                key={skill.name}
                href={APP_ROUTES.marketplace.skill(marketplace, skillSlug)}
                style={{ textDecoration: 'none' }}
              >
                <Stack padding="comfortable" gap="tight">
                  <Text weight="emphasis">{skill.name}</Text>
                  <Text size="supporting" color="muted">
                    {skill.description}
                  </Text>
                </Stack>
              </Link>
            );
          })}
        </Stack>
      )}
    </Stack>
  );
}
