'use client';

/**
 * @tier 3 · agents/[slug]-scoped. The Grades tab: the agent's Graders from `GradersList`, or one
 * Grader's `GraderPage` once picked, each read from the API.
 */

import { Button, EmptyState, Stack, Text } from '@runkora/hades';
import { useAgentGrader, useAgentGraders } from '../_hooks/use-agent-graders';
import { GradersList } from './grader-list';
import { BackButton, GraderPage } from './grader-page';

function ReadError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <EmptyState
      icon="triangle-alert"
      title="Failed to load Graders"
      description={message}
      action={
        <Button variant="outline" size="sm" onClick={onRetry}>
          Retry
        </Button>
      }
    />
  );
}

/** The Grader page, read from the API. */
function GraderRead({
  agentTypeId,
  name,
  slug,
  onBack,
}: {
  agentTypeId: string;
  name: string;
  slug: string;
  onBack: () => void;
}) {
  const { data, error, loading, reload } = useAgentGrader(agentTypeId, name);
  if (data) return <GraderPage grader={data} slug={slug} onBack={onBack} />;
  return (
    <Stack gap="comfortable">
      <BackButton onBack={onBack} />
      {loading ? (
        <Text size="supporting" color="muted">
          Loading Grader…
        </Text>
      ) : (
        <ReadError message={error ?? 'Grader not found'} onRetry={reload} />
      )}
    </Stack>
  );
}

/** The Grades tab: every Grader, or one Grader once picked. */
export function AgentGradesTab({
  agentTypeId,
  slug,
  grader,
  onGrader,
}: {
  agentTypeId: string;
  slug: string;
  /** The open Grader's name; undefined for the list. */
  grader: string | undefined;
  onGrader: (name: string | undefined) => void;
}) {
  // The list is read only while it shows.
  const { data, error, loading, reload } = useAgentGraders(grader ? undefined : agentTypeId);
  if (grader)
    return (
      <GraderRead
        key={grader}
        agentTypeId={agentTypeId}
        name={grader}
        slug={slug}
        onBack={() => onGrader(undefined)}
      />
    );
  if (loading)
    return (
      <Text size="supporting" color="muted">
        Loading Graders…
      </Text>
    );
  if (error) return <ReadError message={error} onRetry={reload} />;
  return <GradersList graders={data ?? []} onOpen={onGrader} />;
}
