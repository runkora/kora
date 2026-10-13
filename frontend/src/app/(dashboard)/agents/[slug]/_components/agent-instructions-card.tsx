'use client';

/** @tier 3 · agents/[slug]-scoped. Instructions card: rendered markdown, clamped with a fade, with a Rendered | Raw toggle. */

import { useEffect, useRef, useState } from 'react';
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CopyButton,
  SegmentedControl,
  Stack,
  Text,
} from '@runkora/hades';
import { CODE_LINE_HEIGHT, InstructionsMarkdown } from './instructions-markdown';

type PromptView = 'rendered' | 'raw';

interface AgentInstructionsCardProps {
  prompt: string;
  /** Opens the Configuration tab at the Behaviour step. */
  onEdit: () => void;
}

const FADE = 'linear-gradient(to bottom, black calc(100% - 4rem), transparent)';

function EditButton({ onEdit }: Pick<AgentInstructionsCardProps, 'onEdit'>) {
  return (
    <Button variant="ghost" size="sm" aria-label="Edit instructions" onClick={onEdit}>
      Edit
    </Button>
  );
}

export function AgentInstructionsCard({ prompt, onEdit }: AgentInstructionsCardProps) {
  const [view, setView] = useState<PromptView>('rendered');
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  // The clamp only offers Expand when the text is taller than the clamp, at any width.
  useEffect(() => {
    const el = box.current;
    if (!el || expanded) return;
    const measure = () => setOverflows(el.scrollHeight > el.clientHeight + 1);
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [prompt, view, expanded]);

  if (!prompt) {
    return (
      <Card>
        <CardHeader compact title="Instructions" divider />
        <CardContent padded>
          <Stack direction="horizontal" align="center" justify="between">
            <Text size="supporting" color="muted">
              No instructions set
            </Text>
            <EditButton onEdit={onEdit} />
          </Stack>
        </CardContent>
      </Card>
    );
  }

  const clamped = overflows && !expanded;
  return (
    <Card>
      <CardHeader
        compact
        title="Instructions"
        description={`${prompt.trim().split(/\s+/).length} words`}
        divider
        action={
          <Stack direction="horizontal" align="center" gap="snug">
            <SegmentedControl
              aria-label="Instructions view"
              value={view}
              onChange={(v) => setView(v as PromptView)}
              options={[
                { value: 'rendered', label: 'Rendered' },
                { value: 'raw', label: 'Raw' },
              ]}
            />
            <CopyButton value={prompt} label="Copy instructions" />
            <EditButton onEdit={onEdit} />
          </Stack>
        }
      />
      <CardContent padded>
        <Stack gap="snug" align="start">
          <Stack
            ref={box}
            align="stretch"
            overflow="hidden"
            maxHeight={expanded ? undefined : 'scroll-lg'}
            style={clamped ? { maskImage: FADE, WebkitMaskImage: FADE } : undefined}
          >
            {view === 'rendered' ? (
              <InstructionsMarkdown content={prompt} />
            ) : (
              <Text
                mono
                size="caption"
                color="muted"
                preserveWhitespace
                style={{ lineHeight: CODE_LINE_HEIGHT }}
              >
                {prompt}
              </Text>
            )}
          </Stack>
          {(overflows || expanded) && (
            <Button
              variant="ghost"
              size="sm"
              aria-expanded={expanded}
              onClick={() => setExpanded((open) => !open)}
            >
              {expanded ? 'Collapse' : 'Expand'}
            </Button>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
