'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. The skill page: header, the open file, and the Files and Source rail. */

import { useEffect, useMemo, useRef, useState } from 'react';
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
import type { Marketplace, Skill, SkillSummary } from '@runkora/shared/api';
import {
  CODE_LINE_HEIGHT,
  InstructionsMarkdown,
} from '../../../../agents/[slug]/_components/instructions-markdown';
import { fetchSkillFile } from '../../../_services/skills-api';
import type { SkillsLoadError } from '../../../_types';
import { formatBytes } from './skill-format';
import { SkillFilesCard } from './skill-files-card';
import { SkillHeader, SourceCard } from './skill-header';
import { SkillLoadError } from './skill-load-error';

const SKILL_MD = 'SKILL.md';

type MarkdownView = 'rendered' | 'raw';

interface SkillDetailProps {
  skill: Skill;
  /** Undefined when the Marketplace list could not be read; the Synced row then goes. */
  marketplace?: Marketplace;
  /** The skill's list entry, for its version and chips. Null when it could not be read. */
  summary: SkillSummary | null;
}

const byteLength = (text: string) => new TextEncoder().encode(text).length;

/** The SKILL.md body without its frontmatter and its first heading, which the header shows. */
function bodyWithoutTitle(content: string): string {
  const frontmatter = /^---\r?\n[\s\S]*?\r?\n---\r?\n?/.exec(content);
  const body = frontmatter ? content.slice(frontmatter[0].length).trimStart() : content;
  return body.replace(/^# .+\r?\n/, '').trimStart();
}

export function SkillDetail({ skill, marketplace, summary }: SkillDetailProps) {
  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  // SKILL.md arrives with the page; each other file loads once, when first opened.
  const [contents, setContents] = useState<Record<string, string>>({ [SKILL_MD]: skill.content });
  const [failures, setFailures] = useState<Record<string, SkillsLoadError | undefined>>({});
  const loading = useRef(new Set<string>());
  const mounted = useRef(false);
  const files = useMemo(
    () => [{ path: SKILL_MD, size: byteLength(skill.content) }, ...skill.files],
    [skill],
  );

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  function open(file: string) {
    setSelectedPath(file);
    // Results are kept per file, so a file already loaded or loading needs no new request.
    if (file in contents || loading.current.has(file)) return;
    loading.current.add(file);
    setFailures((prev) => ({ ...prev, [file]: undefined }));
    void fetchSkillFile({ marketplace: skill.marketplace, path: skill.path, file }).then(
      (result) => {
        loading.current.delete(file);
        if (!mounted.current) return;
        if (result.data) setContents((prev) => ({ ...prev, [file]: result.data.content }));
        else setFailures((prev) => ({ ...prev, [file]: result.error }));
      },
    );
  }

  const selected = files.find((file) => file.path === selectedPath) ?? files[0];
  const filesCard = <SkillFilesCard files={files} selectedPath={selectedPath} onSelect={open} />;
  const sourceCard = <SourceCard skill={skill} marketplace={marketplace} />;

  return (
    <Stack gap="relaxed">
      <SkillHeader skill={skill} summary={summary} />

      {/* Below xl the rail would squeeze the file to a narrow column, so it moves above it. */}
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
            content={contents[selected.path]}
            failure={failures[selected.path]}
            onRetry={() => open(selected.path)}
            onBack={() => setSelectedPath(SKILL_MD)}
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
      {/* CodeBlock carries its own copy button, so only Markdown needs one here. */}
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
          <SkillLoadError status={failure.status} message={failure.message} onRetry={onRetry} />
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
