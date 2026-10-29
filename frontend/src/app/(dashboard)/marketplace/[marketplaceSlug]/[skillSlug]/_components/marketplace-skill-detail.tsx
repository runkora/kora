'use client';

/** @tier 3 — marketplace/[marketplaceSlug]/[skillSlug]-scoped. Read-only presentation of one Skill's details. */

import { useMemo, useState } from 'react';
import { CodeBlock, Stack } from '@orbit/hades';
import type { Skill } from '@orbit/shared/api';
import { SkillFileExplorer } from './skill-file-explorer';
import type { ExplorerFile } from './skill-file-explorer';
import { SkillRepositoryCard } from './skill-repository-card';

interface MarketplaceSkillDetailProps {
  skill: Skill;
  /** The GitHub repository link of the Skill's Marketplace. */
  repositoryUrl: string;
}

/** The path of a file relative to the Skill's folder (the folder holding SKILL.md). */
function relativeTo(folder: string, path: string): string {
  return folder && path.startsWith(`${folder}/`) ? path.slice(folder.length + 1) : path;
}

export function MarketplaceSkillDetail({ skill, repositoryUrl }: MarketplaceSkillDetailProps) {
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
        <SkillRepositoryCard repositoryUrl={repositoryUrl} />
      </Stack>
    </Stack>
  );
}
