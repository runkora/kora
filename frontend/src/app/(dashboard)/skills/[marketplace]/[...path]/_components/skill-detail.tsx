'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. The skill page: header, the open file, and the Files and Source rail. */

import { useState } from 'react';
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CodeBlock,
  CopyButton,
  Grid,
  Icon,
  SegmentedControl,
  Show,
  Skeleton,
  Stack,
  Text,
} from '@runkora/hades';
import { CODE_LINE_HEIGHT, InstructionsMarkdown } from '@runkora/components';
import type { Marketplace, Skill } from '@runkora/shared/api';
import { LoadErrorAlert } from '../../../_components/load-error-alert';
import { formatBytes } from '../../../_components/skill-format';
import { SKILL_MD, useSkillFiles } from '../../../_hooks/use-skill-files';
import type { SkillsLoadError } from '../../../_types';
import { SkillFilesCard } from './skill-files-card';
import { SkillHeader, SourceCard } from './skill-header';

type MarkdownView = 'rendered' | 'raw';

interface SkillDetailProps {
  skill: Skill;
  /** Undefined when the Marketplace list could not be read; the Synced row then goes. */
  marketplace?: Marketplace;
}

/** The SKILL.md body without the frontmatter and first heading the header already shows. */
function bodyWithoutTitle(content: string): string {
  const frontmatter = /^---\r?\n[\s\S]*?\r?\n---\r?\n?/.exec(content);
  const body = frontmatter ? content.slice(frontmatter[0].length).trimStart() : content;
  return body.replace(/^# .+\r?\n/, '').trimStart();
}

export function SkillDetail({ skill, marketplace }: SkillDetailProps) {
  const { files, selected, content, failure, open } = useSkillFiles(skill);
  const filesCard = <SkillFilesCard files={files} selectedPath={selected.path} onSelect={open} />;
  const sourceCard = <SourceCard skill={skill} marketplace={marketplace} />;

  return (
    <Stack gap="relaxed">
      <SkillHeader skill={skill} />

      {/* Below xl the rail would squeeze the file to a narrow column; it moves above it. */}
      <Show below="xl">
        <Grid cols={{ base: 1, md: 2 }} gap="comfortable">
          {filesCard}
          {sourceCard}
        </Grid>
      </Show>

      <Stack direction="horizontal" align="start" gap="relaxed">
        <Stack grow minWidth="none">
          <OpenFile
            path={selected.path}
            size={selected.size}
            content={content}
            failure={failure}
            onRetry={() => open(selected.path)}
            onBack={() => open(SKILL_MD)}
          />
        </Stack>
        <Show above="xl">
          <Stack gap="comfortable" minWidth="detail-panel" maxWidth="detail-panel">
            {filesCard}
            {sourceCard}
          </Stack>
        </Show>
      </Stack>
    </Stack>
  );
}

interface OpenFileProps {
  path: string;
  size: number;
  /** Undefined while the file loads or after it failed. */
  content: string | undefined;
  failure: SkillsLoadError | undefined;
  onRetry: () => void;
  onBack: () => void;
}

/** SKILL.md, or the file picked in the tree. Markdown gets Rendered and Raw; any other file is plain text. */
function OpenFile({ path, size, content, failure, onRetry, onBack }: OpenFileProps) {
  const [view, setView] = useState<MarkdownView>('rendered');
  const isSkillMd = path === SKILL_MD;
  const isMarkdown = path.toLowerCase().endsWith('.md');
  const lines = content === undefined ? null : content.replace(/\n$/, '').split('\n').length;

  const actions = (
    <Stack direction="horizontal" align="center" gap="snug">
      {isMarkdown && content !== undefined && (
        <SegmentedControl
          aria-label="File view"
          value={view}
          onChange={(value) => setView(value as MarkdownView)}
          options={[
            { value: 'rendered', label: 'Rendered' },
            { value: 'raw', label: 'Raw' },
          ]}
        />
      )}
      {isMarkdown && content !== undefined && <CopyButton value={content} label={`Copy ${path}`} />}
      {!isSkillMd && (
        <Button variant="ghost" size="sm" onClick={onBack}>
          <Icon name="arrow-left" size="xs" />
          SKILL.md
        </Button>
      )}
    </Stack>
  );

  return (
    <Card>
      <CardHeader
        compact
        leadingIcon={isMarkdown ? 'file' : 'file-code'}
        title={path}
        description={lines === null ? formatBytes(size) : `${formatBytes(size)} · ${lines} lines`}
        divider
        action={actions}
      />
      <CardContent padded>
        {failure ? (
          <LoadErrorAlert error={failure} onRetry={onRetry} />
        ) : content === undefined ? (
          <Stack role="status" aria-label="Loading the file" gap="snug">
            <Skeleton shape="line" width="full" />
            <Skeleton shape="line" width="full" />
            <Skeleton shape="block" width="full" size="xl" />
          </Stack>
        ) : isMarkdown && view === 'rendered' ? (
          <InstructionsMarkdown content={isSkillMd ? bodyWithoutTitle(content) : content} />
        ) : isMarkdown ? (
          <Text
            mono
            size="caption"
            color="muted"
            preserveWhitespace
            style={{ lineHeight: CODE_LINE_HEIGHT }}
          >
            {content}
          </Text>
        ) : (
          <CodeBlock key={path} text={content} />
        )}
      </CardContent>
    </Card>
  );
}
