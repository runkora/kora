'use client';

/** @tier 3 — agents-scoped. The caller's agent drafts: a header button with a count and an expandable list. */

// A draft is not an agent type, so it does not arrive with the agents list —
// the caller's drafts are fetched separately and offer resume and delete,
// nothing else (docs/adr/2026-07-17-agent-builder.md §2).

import { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { Badge, Button, Card, CardContent, Eyebrow, Icon, Stack, Text } from '@orbit/terra';
import type { AgentTypeDraft } from '@orbit/shared/api';
import { deleteDraft, fetchDrafts } from '../_services/agents-api';
import { openBuilder } from '../_lib/builder-url';

function draftName(draft: AgentTypeDraft): string {
  const name = (draft.payload as { name?: string }).name;
  return name?.trim() ? name : 'Untitled agent';
}

function since(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

/** The caller's drafts, refreshed on mount and whenever the builder closes. */
export function useDrafts() {
  const [drafts, setDrafts] = useState<AgentTypeDraft[]>([]);
  const searchParams = useSearchParams();
  const builderOpen = searchParams.get('agent') !== null;

  const refresh = useCallback(() => {
    void fetchDrafts().then(({ data }) => setDrafts(data));
  }, []);

  // A draft may have been created, updated, or turned into an agent while the builder was open.
  useEffect(() => {
    if (!builderOpen) refresh();
  }, [builderOpen, refresh]);

  return { drafts, refresh };
}

export interface DraftsButtonProps {
  count: number;
  open: boolean;
  onToggle: () => void;
}

/** Renders nothing when there are no drafts. */
export function DraftsButton({ count, open, onToggle }: DraftsButtonProps) {
  if (count === 0) return null;
  return (
    <Button variant="ghost" aria-expanded={open} onClick={onToggle}>
      <Icon name="file-text" size="xs" />
      Drafts
      <Badge variant="secondary" size="sm">
        {count}
      </Badge>
    </Button>
  );
}

export interface DraftsListProps {
  drafts: AgentTypeDraft[];
  onChanged: () => void;
}

export function DraftsList({ drafts, onChanged }: DraftsListProps) {
  return (
    <Stack gap="cozy">
      <Stack direction="horizontal" align="baseline" gap="snug">
        <Eyebrow>Drafts</Eyebrow>
        <Text as="span" size="micro" color="subtle">
          Saved builder sessions, visible only to you
        </Text>
      </Stack>
      <Stack gap="snug">
        {drafts.map((draft) => (
          <Card key={draft.id} subtle>
            <CardContent padded compact>
              <Stack direction="horizontal" align="center" gap="cozy">
                <Icon
                  name="file-text"
                  size="sm"
                  color="subtle"
                  container
                  containerVariant="surface-subtle"
                  label=""
                />
                <Stack grow gap="tight">
                  <Text as="span" size="supporting" weight="heavy">
                    {draftName(draft)}
                  </Text>
                  <Text as="span" size="micro" color="subtle">
                    draft · edited {since(draft.updated_at)}
                  </Text>
                </Stack>
                <Button variant="secondary" size="sm" onClick={() => openBuilder(draft.id)}>
                  Resume
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Delete draft ${draftName(draft)}`}
                  onClick={() => {
                    void deleteDraft(draft.id).then(onChanged);
                  }}
                >
                  <Icon name="trash" />
                </Button>
              </Stack>
            </CardContent>
          </Card>
        ))}
      </Stack>
    </Stack>
  );
}
