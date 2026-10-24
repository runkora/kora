'use client';

/** @tier 3 — agents-scoped. One agent in the grid: identity, status, description, spend share and run controls. */

import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  Icon,
  Progress,
  Stack,
  Text,
} from '@runkora/hades';
import { ManagedInGitBadge, PermissionHint, StatusBadge } from '@runkora/components';
import { fmtUsd } from '@runkora/utils';
import type { AgentTypeUsage } from '@runkora/shared/api';
import { APP_ROUTES } from '@runkora/lib/routing';
import type { AgentTypeRow } from '../_types';

const STATUS: Record<string, 'healthy' | 'degraded' | 'error'> = {
  active: 'healthy',
  provisioning: 'degraded',
  deleting: 'error',
};

const STATUS_LABEL: Partial<Record<string, string>> = {
  provisioning: 'Provisioning',
  deleting: 'Deleting',
};

export interface AgentCardProps {
  agent: AgentTypeRow;
  usage: AgentTypeUsage | undefined;
  /** Spend across every agent, so the bar shows this agent's share of it. */
  totalCost: number;
  /** Null when the run counts could not be read. */
  liveRuns: number | null;
  /** Short name for the window the cost covers, for example "7d". */
  period: string;
  /** Shown in place of the usage line while the figures are loading or unavailable. */
  usageNote?: string | undefined;
  onRun: (agent: AgentTypeRow) => void;
}

export function AgentCard({
  agent,
  usage,
  totalCost,
  liveRuns,
  period,
  usageNote,
  onRun,
}: AgentCardProps) {
  const router = useRouter();
  const cost = usage?.estimated_cost_usd ?? 0;
  const share = totalCost > 0 ? (cost / totalCost) * 100 : 0;
  const runnable = agent.state === 'active';

  return (
    // The whole card is a mouse target; the settings link is the keyboard route to the same page,
    // so the card is not given a button role that would nest the buttons inside it.
    <Card
      column
      onClick={() => router.push(APP_ROUTES.agents.detail(agent.slug))}
      style={{ cursor: 'pointer' }}
    >
      <CardHeader
        leadingIcon="bot"
        title={agent.name}
        description={
          <Stack direction="horizontal" align="center" gap="snug">
            <Text mono size="caption" color="muted">
              {agent.slug}
            </Text>
            {agent.identityMode === 'delegated' && (
              <Text size="caption" color="subtle">
                Delegated
              </Text>
            )}
            {agent.declared && <ManagedInGitBadge />}
          </Stack>
        }
        action={
          <StatusBadge
            status={STATUS[agent.state] ?? 'unknown'}
            label={STATUS_LABEL[agent.state]}
          />
        }
        divider
      />

      <CardContent padded grow>
        <Text
          size="supporting"
          color="muted"
          style={{
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
          }}
        >
          {agent.description || 'No description.'}
        </Text>
      </CardContent>

      <CardFooter onClick={(e) => e.stopPropagation()}>
        <Stack grow gap="cozy">
          <Stack gap="snug">
            <Stack direction="horizontal" align="baseline" justify="between">
              <Text size="caption" color="muted">
                {agent.framework}
              </Text>
              <Text size="caption" color="muted" mono tabular>
                {usageNote ?? (usage ? `${fmtUsd(cost)} · ${period}` : 'No usage yet')}
              </Text>
            </Stack>
            <Progress
              size="sm"
              value={share}
              label={`${agent.name} share of total spend`}
              showValue
            />
          </Stack>
          <Stack direction="horizontal" align="center" justify="between">
            <Text size="caption" color="subtle">
              {liveRuns === null
                ? 'Run count unavailable'
                : liveRuns > 0
                  ? `${liveRuns} running now`
                  : 'Idle'}
            </Text>
            <Stack direction="horizontal" align="center" gap="tight">
              <PermissionHint allowed={agent.canRun} action="start runs of this agent">
                <Button
                  size="sm"
                  disabled={!runnable || !agent.canRun}
                  onClick={() => onRun(agent)}
                >
                  <Icon name="play" size="xs" />
                  Run
                </Button>
              </PermissionHint>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Settings for ${agent.name}`}
                asChild
              >
                <Link href={APP_ROUTES.agents.detail(agent.slug)}>
                  <Icon name="settings" size="xs" />
                </Link>
              </Button>
            </Stack>
          </Stack>
        </Stack>
      </CardFooter>
    </Card>
  );
}
