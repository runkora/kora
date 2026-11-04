'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. One skill's files and its source repository. */

import { useEffect, useRef, useState } from 'react';
import { Card, CardContent, CodeBlock, IconTile, Skeleton, Stack, Text } from '@runkora/hades';
import type { Skill } from '@runkora/shared/api';
import { fetchSkillFile } from '../../../_services/skills-api';
import type { SkillsLoadError } from '../../../_types';
import { SkillFileExplorer } from './skill-file-explorer';
import { SkillLoadError } from './skill-load-error';

const SKILL_MD = 'SKILL.md';

export function SkillDetail({ skill, marketplace }: { skill: Skill; marketplace: string }) {
  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  // SKILL.md arrives with the page; each other file loads once, when first selected.
  const [contents, setContents] = useState<Record<string, string>>({ [SKILL_MD]: skill.content });
  const [failures, setFailures] = useState<Record<string, SkillsLoadError | undefined>>({});
  const inFlight = useRef<AbortController | null>(null);
  const files = [
    { path: SKILL_MD, size: new TextEncoder().encode(skill.content).length },
    ...skill.files,
  ];
  const owner = skill.repository.split('/')[0];

  useEffect(() => {
    const requests = inFlight;
    return () => requests.current?.abort();
  }, []);

  function open(file: string) {
    setSelectedPath(file);
    if (file in contents) return;
    setFailures((prev) => ({ ...prev, [file]: undefined }));
    // Only the latest selection may land, so picking another file aborts this one.
    inFlight.current?.abort();
    const controller = new AbortController();
    inFlight.current = controller;
    void fetchSkillFile({ marketplace, path: skill.path, file }, controller.signal).then(
      (result) => {
        if (controller.signal.aborted) return;
        if (result.data) setContents((prev) => ({ ...prev, [file]: result.data.content }));
        else setFailures((prev) => ({ ...prev, [file]: result.error }));
      },
    );
  }

  const content = contents[selectedPath];
  const failure = failures[selectedPath];

  return (
    <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="relaxed">
      <Stack gap="comfortable" grow minWidth="none">
        <SkillFileExplorer files={files} selectedPath={selectedPath} onSelect={open} />
        {content !== undefined ? (
          <CodeBlock key={selectedPath} filename={selectedPath} text={content} wrap />
        ) : failure ? (
          <SkillLoadError
            status={failure.status}
            message={failure.message}
            onRetry={() => open(selectedPath)}
          />
        ) : (
          <Stack role="status" aria-label="Loading the file">
            <Skeleton shape="block" width="full" size="xl" />
          </Stack>
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
