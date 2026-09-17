'use client';

/** @tier 3 — marketplace-scoped. One Skill result; the whole card opens its detail route. */

import { useState } from 'react';
import Link from 'next/link';
import { Card, Icon, Stack, Text } from '@orbit/terra';
import type { PackageOverview } from '@orbit/shared/api';
import { APP_ROUTES } from '@orbit/lib/routing';

interface SkillCardProps {
  skill: PackageOverview;
}

export function SkillCard({ skill }: SkillCardProps) {
  const [hover, setHover] = useState(false);
  const [org, repo] = skill.slug.split('/');

  return (
    <Link
      href={APP_ROUTES.marketplace.detail(skill.slug)}
      style={{ textDecoration: 'none', color: 'inherit' }}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <Card
        style={{
          borderColor: hover ? 'var(--color-accent)' : undefined,
          transition: 'border-color 150ms, background 150ms',
        }}
      >
        <Stack
          gap="cozy"
          style={{
            padding: '16px 16px 14px',
            cursor: 'pointer',
            background: hover ? 'var(--color-surface-elevated)' : 'transparent',
            transition: 'background 150ms',
          }}
        >
          <Stack gap="none" style={{ minWidth: 0 }}>
            <Text as="span" weight="strong" style={{ fontSize: 15, letterSpacing: '-0.01em' }}>
              {skill.title_of_package}
            </Text>
            <Text
              as="span"
              mono
              size="caption"
              weight="emphasis"
              color="subtle"
              truncate
              style={{ marginTop: 1, lineHeight: 1.5 }}
            >
              {skill.slug}
            </Text>
          </Stack>

          <Text color="muted" style={{ fontSize: 13, lineHeight: 1.5, minHeight: 38 }}>
            {skill.description || 'No description provided.'}
          </Text>
        </Stack>

        <Stack
          direction="horizontal"
          align="center"
          justify="between"
          style={{ padding: '10px 16px', borderTop: '1px solid var(--color-border-subtle)' }}
        >
          <Text as="span" size="caption" color="subtle">
            <Text as="span" size="caption" color="muted" weight="strong">
              {org}
            </Text>{' '}
            · {repo}
          </Text>
          <Icon name="chevron-right" size="2xs" color="muted" />
        </Stack>
      </Card>
    </Link>
  );
}
