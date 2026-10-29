'use client';

/** @tier 3 — marketplace/[marketplaceSlug]/[skillName]-scoped. Read-only presentation of one Skill's details. */

import { useMemo, useState } from 'react';
import { Card, CardContent, CodeBlock, IconTile, Stack, Text } from '@orbit/hades';
import type { Skill } from '@orbit/shared/api';
import { SkillFileExplorer } from './skill-file-explorer';
import type { ExplorerFile } from './skill-file-explorer';

interface MarketplaceSkillDetailProps {
  skill: Skill;
  /** The GitHub repository link of the Skill's Marketplace. */
  repositoryUrl: string;
}

/** The path of a file relative to the Skill's folder (the folder holding SKILL.md). */
function relativeTo(folder: string, path: string): string {
  return folder && path.startsWith(`${folder}/`) ? path.slice(folder.length + 1) : path;
}

/** The `owner` and `owner/repo` of a GitHub repository link, or the raw link when it has no such path. */
export function repositoryName(link: string): { owner: string; fullName: string } {
  try {
    const [owner, repo] = new URL(link).pathname.split('/').filter(Boolean);
    if (owner && repo) return { owner, fullName: `${owner}/${repo.replace(/\.git$/, '')}` };
  } catch {
    // Not a URL; fall through to the raw link.
  }
  return { owner: link, fullName: link };
}

export function MarketplaceSkillDetail({ skill, repositoryUrl }: MarketplaceSkillDetailProps) {
  const { owner, fullName } = repositoryName(repositoryUrl);
  const slash = skill.skill.path.lastIndexOf('/');
  const folder = slash >= 0 ? skill.skill.path.slice(0, slash) : '';
  const files = useMemo<ExplorerFile[]>(
    () =>
      [skill.skill, ...skill.documents].map((file) => ({
        relativePath: relativeTo(folder, file.path),
        file,
      })),
    [skill, folder],
  );

  const [selectedPath, setSelectedPath] = useState(files[0].relativePath);
  const selected = files.find((f) => f.relativePath === selectedPath) ?? files[0];

  return (
    <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="relaxed" align="start">
      <Stack gap="comfortable" grow minWidth="none" style={{ width: '100%' }}>
        <SkillFileExplorer
          files={files}
          selectedPath={selected.relativePath}
          onSelect={setSelectedPath}
        />

        <CodeBlock
          key={selected.relativePath}
          filename={selected.relativePath}
          text={selected.file.content}
          wrap
        />
      </Stack>
      <Stack
        shrink
        minWidth="none"
        maxWidth={{ base: 'full', lg: 'detail-panel' }}
        // Clears the sticky back-to-Skills bar (48px) plus the page gap.
        style={{ width: '100%', position: 'sticky', top: 64 }}
      >
        <Card column>
          <CardContent padded>
            <Stack direction="horizontal" align="center" gap="cozy">
              <IconTile size="lg" aria-hidden>
                <Text as="span" size="supporting" weight="heavy">
                  {owner.charAt(0).toUpperCase()}
                </Text>
              </IconTile>
              <Stack grow minWidth="none" gap="none">
                <Text as="span" size="supporting" weight="heavy" truncate>
                  {owner}
                </Text>
                <Text as="span" size="caption" color="subtle" truncate>
                  {fullName}
                </Text>
              </Stack>
            </Stack>
          </CardContent>
        </Card>
      </Stack>
    </Stack>
  );
}
