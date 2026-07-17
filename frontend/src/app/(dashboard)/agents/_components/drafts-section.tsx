'use client';

/** @tier 3 — agents-scoped. The caller's agent drafts, listed above the agent table. */

// A draft is not an agent type, so it does not arrive with the agents list —
// this section fetches the caller's drafts separately and offers resume and
// delete, nothing else (docs/adr/2026-07-17-agent-builder.md §2).

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { usePathname, useSearchParams } from 'next/navigation';
import { Button, Card, CardContent, Icon, Stack, Text } from '@orbit/terra';
import type { AgentTypeDraft } from '@orbit/shared/api';
import { deleteDraft, fetchDrafts } from '../_services/agents-api';

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

export function DraftsSection() {
  const [drafts, setDrafts] = useState<AgentTypeDraft[]>([]);
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const builderOpen = searchParams.get('agent') !== null;

  const refresh = useCallback(() => {
    void fetchDrafts().then(({ data }) => setDrafts(data));
  }, []);

  // Refresh on mount and whenever the builder closes (a draft may have been
  // created, updated, or turned into an agent while it was open).
  useEffect(() => {
    if (!builderOpen) refresh();
  }, [builderOpen, refresh]);

  if (drafts.length === 0) return null;

  return (
    <Stack gap="cozy">
      <Stack direction="horizontal" align="baseline" gap="snug">
        <Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase">
          Drafts
        </Text>
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
                <Button asChild variant="secondary" size="sm">
                  <Link href={`${pathname}?agent=${draft.id}`}>Resume</Link>
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Delete draft ${draftName(draft)}`}
                  onClick={() => {
                    void deleteDraft(draft.id).then(refresh);
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
