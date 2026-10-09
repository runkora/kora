'use client';

import { useState } from 'react';

/** @tier 3 — triggers-scoped. Shared Target step: which agent this trigger
 * runs. How its runs queue belongs with the rest of the queue, on Delivery. */

import {
  Button,
  Callout,
  Card,
  CardContent,
  Icon,
  IconTile,
  Input,
  RadioGroup,
  RadioGroupItem,
  SectionLabel,
  Stack,
  Text,
} from '@orbit/hades';
import type { StepComponentProps, TriggerWizardAgent } from './types';

function initial(agent: TriggerWizardAgent): string {
  const source = agent.name?.trim() || agent.slug;
  return (source.charAt(0) || '?').toUpperCase();
}

/** Above this many agents the list gains a search box and pages. */
const AGENT_SEARCH_THRESHOLD = 6;

/** Agents shown per page: enough to scan, few enough to never scroll. */
const AGENT_PAGE_SIZE = 5;

export function StepTarget({ state, patch, context }: StepComponentProps) {
  const { agents } = context;
  const [agentQuery, setAgentQuery] = useState('');
  const [agentPage, setAgentPage] = useState(0);
  const query = agentQuery.trim().toLowerCase();
  const matchingAgents =
    query === ''
      ? agents
      : agents.filter(
          (a) =>
            a.slug.toLowerCase().includes(query) || (a.name ?? '').toLowerCase().includes(query),
        );
  // Paged, never scrolled: a scroll region inside the step's own scroll was
  // two nested scrollbars, and flexbox clipping besides.
  const pageCount = Math.max(1, Math.ceil(matchingAgents.length / AGENT_PAGE_SIZE));
  const page = Math.min(agentPage, pageCount - 1);
  const visibleAgents = matchingAgents.slice(
    page * AGENT_PAGE_SIZE,
    page * AGENT_PAGE_SIZE + AGENT_PAGE_SIZE,
  );
  return (
    <Stack gap="relaxed">
      <Stack gap="cozy">
        <SectionLabel>Agent to run</SectionLabel>
        {agents.length === 0 && (
          <Callout variant="warning">
            No agents are available to bind. Create an autonomous agent first.
          </Callout>
        )}
        {agents.length > AGENT_SEARCH_THRESHOLD && (
          <Stack direction="horizontal" align="center" gap="cozy">
            <Stack grow maxWidth="search-field">
              <Input
                compact
                value={agentQuery}
                onChange={(e) => {
                  setAgentQuery(e.target.value);
                  setAgentPage(0);
                }}
                placeholder="Search agents…"
                aria-label="Search agents"
              />
            </Stack>
            <Text as="span" size="caption" color="subtle">
              {matchingAgents.length} of {agents.length}
            </Text>
          </Stack>
        )}
        <RadioGroup
          value={state.agentTypeId}
          onValueChange={(v) => patch({ agentTypeId: v })}
          aria-label="Agent"
        >
          <Stack gap="snug">
            {visibleAgents.length === 0 && (
              <Text size="caption" color="subtle">
                No agents match this search.
              </Text>
            )}
            {visibleAgents.map((a) => {
              const delegated = a.identityMode === 'delegated';
              return (
                <Card
                  key={a.id}
                  interactive={!delegated}
                  subtle={delegated}
                  selected={state.agentTypeId === a.id}
                  onClick={delegated ? undefined : () => patch({ agentTypeId: a.id })}
                >
                  <CardContent padded compact>
                    <Stack direction="horizontal" align="center" gap="cozy">
                      <IconTile tone={state.agentTypeId === a.id ? 'accent' : 'muted'}>
                        <Text as="span" size="caption" mono weight="heavy">
                          {initial(a)}
                        </Text>
                      </IconTile>
                      <Stack gap="tight" grow minWidth="none">
                        <Text as="span" size="supporting" weight="heavy">
                          {a.name?.trim() || a.slug}
                        </Text>
                        <Text as="span" size="micro" mono color="subtle">
                          {a.slug}
                        </Text>
                      </Stack>
                      {delegated ? (
                        <Text as="span" size="micro" color="subtle">
                          delegated · started by people
                        </Text>
                      ) : (
                        <RadioGroupItem value={a.id} aria-label={a.name?.trim() || a.slug} />
                      )}
                    </Stack>
                  </CardContent>
                </Card>
              );
            })}
          </Stack>
        </RadioGroup>
        {pageCount > 1 && (
          <Stack direction="horizontal" align="center" justify="end" gap="snug">
            <Text as="span" size="caption" color="subtle">
              {page * AGENT_PAGE_SIZE + 1}–{page * AGENT_PAGE_SIZE + visibleAgents.length} of{' '}
              {matchingAgents.length}
            </Text>
            <Button
              variant="ghost"
              size="icon-sm"
              disabled={page === 0}
              aria-label="Previous agents"
              onClick={() => setAgentPage(page - 1)}
            >
              <Icon name="chevron-left" size="2xs" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              disabled={page >= pageCount - 1}
              aria-label="More agents"
              onClick={() => setAgentPage(page + 1)}
            >
              <Icon name="chevron-right" size="2xs" />
            </Button>
          </Stack>
        )}
      </Stack>
    </Stack>
  );
}
