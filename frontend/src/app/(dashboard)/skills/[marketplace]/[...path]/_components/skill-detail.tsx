'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. One skill's files and its source repository. */

import { useState } from 'react';
import { Card, CardContent, CodeBlock, IconTile, Stack, Text } from '@runkora/hades';
import type { Skill } from '@runkora/shared/api';
import { SkillFileExplorer } from './skill-file-explorer';

export function SkillDetail({ skill }: { skill: Skill }) {
  const [selectedPath, setSelectedPath] = useState(skill.files[0]?.path ?? '');
  const selected = skill.files.find((file) => file.path === selectedPath) ?? skill.files[0];
  const owner = skill.repository.split('/')[0];

  return (
    <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="relaxed">
      <Stack gap="comfortable" grow minWidth="none">
        <SkillFileExplorer
          files={skill.files}
          selectedPath={selectedPath}
          onSelect={setSelectedPath}
        />
        {selected && (
          <CodeBlock key={selected.path} filename={selected.path} text={selected.content} wrap />
        )}
      </Stack>

      <Stack
        minWidth={{ base: 'none', lg: 'detail-panel' }}
        maxWidth={{ base: 'full', lg: 'detail-panel' }}
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
                  {skill.repository}
                </Text>
              </Stack>
            </Stack>
          </CardContent>
        </Card>
      </Stack>
    </Stack>
  );
}
