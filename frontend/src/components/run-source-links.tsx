'use client';

/** @tier 2 — shared. Icon links to what started a run, used by the Runs tables and the run view top bar. */

import { Icon, Stack, Text } from '@runkora/hades';
import type { RunSource } from '@runkora/shared/api';

const ICON_BY_KIND = {
  trigger: 'zap',
  pull_request: 'git-branch',
  branch: 'git-branch',
  repository: 'folder',
} as const;

export interface RunSourceLinksProps {
  sources?: RunSource[] | null;
}

export function RunSourceLinks({ sources }: RunSourceLinksProps) {
  if (!sources || sources.length === 0) return null;
  return (
    <Stack direction="horizontal" align="center" gap="tight">
      {sources.map((source) => (
        <a
          key={`${source.kind}:${source.url}`}
          href={source.url}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={`${source.label} (opens in a new tab)`}
          style={{ display: 'inline-flex', alignItems: 'center', gap: 4, textDecoration: 'none' }}
        >
          <Icon
            name={ICON_BY_KIND[source.kind] ?? 'external-link'}
            size="2xs"
            color="accent"
            label=""
          />
          <Text as="span" size="supporting" color="muted">
            {source.label}
          </Text>
        </a>
      ))}
    </Stack>
  );
}
