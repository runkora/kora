'use client';

/**
 * @tier 3 — run-page-scoped. The run page's header: source links, branch, title, actions,
 * elapsed time, spend, and the run's state with its progress through the plan.
 */

import Link from 'next/link';
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Heading,
  Icon,
  Progress,
  Stack,
  Text,
  type IconName,
} from '@runkora/hades';
import { RunSourceChips } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useRunLayout } from '../_lib/run-layout';
import { clockOf, ENDED, formatSpan, type RunView } from '../_lib/run-view';
import { branchName, NOWRAP } from '../_lib/run-styles';

function Figure({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <Stack gap="tight" align="end">
      <Text size="caption" color="subtle">
        {label}
      </Text>
      <Text size="lead" weight="strong" tabular style={{ lineHeight: 1.1 }}>
        {value}
        {sub && (
          <Text as="span" size="supporting" color="subtle" weight="regular">
            {' '}
            {sub}
          </Text>
        )}
      </Text>
    </Stack>
  );
}

function planProgress(run: RunView): { done: number; total: number } | null {
  const plan = [...run.entries].reverse().find((e) => e.type === 'plan');
  if (!plan || plan.type !== 'plan' || plan.plan.items.length === 0) return null;
  return {
    done: plan.plan.items.filter((i) => i.status === 'done').length,
    total: plan.plan.items.length,
  };
}

/**
 * The run's state in a word or two, with the icon and colour the header and end line share.
 * `null` while the state is unknown, so nothing is shown rather than a wrong state.
 */
export function stateLabel(run: RunView): {
  label: string;
  icon: IconName;
  tone: 'accent' | 'status-error' | 'muted';
} | null {
  if (run.state === undefined) return null;
  switch (run.state) {
    case 'active':
      return { label: 'Working', icon: 'loader', tone: 'accent' };
    case 'idle':
      return { label: 'Waiting for a reply', icon: 'pause', tone: 'muted' };
    case 'pending':
    case 'starting':
      return { label: 'Starting', icon: 'loader', tone: 'muted' };
    case 'completing':
      return { label: 'Finishing', icon: 'loader', tone: 'muted' };
    case 'budget_exhausted':
      return { label: 'Budget spent', icon: 'circle-alert', tone: 'status-error' };
    case 'failed':
      return { label: 'Failed', icon: 'circle-alert', tone: 'status-error' };
    case 'terminated':
      return { label: 'Stopped', icon: 'circle-check', tone: 'muted' };
    default:
      return { label: 'Completed', icon: 'circle-check', tone: 'muted' };
  }
}

/** The "…" menu a phone shows in place of the Trace button. */
function MoreMenu({ traceUrl }: { traceUrl: string }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="icon-sm" variant="ghost" aria-label="More actions">
          <Icon name="more-horizontal" size="2xs" label="" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem asChild>
          <Link href={traceUrl} target="_blank" rel="noopener noreferrer">
            <Icon name="activity" size="2xs" label="" />
            Trace
          </Link>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function RunHeader({
  run,
  canRun,
  onSessions,
  onDetails,
}: {
  run: RunView;
  canRun: boolean;
  /** Opens the sessions drawer when the pane is folded away. */
  onSessions?: () => void;
  /** Opens the details sheet when the rail is folded away. */
  onDetails?: () => void;
}) {
  const layout = useRunLayout();
  const narrow = layout === 'tablet' || layout === 'phone';
  const phone = layout === 'phone';
  const progress = planProgress(run);
  const ended = run.state !== undefined && ENDED.has(run.state);
  const budget = run.state === 'budget_exhausted';
  const branch = run.sources.find((s) => s.kind === 'branch');
  const status = stateLabel(run);
  return (
    <Stack gap="comfortable" style={{ padding: phone ? '14px 16px 0' : '22px 32px 0' }}>
      <Stack direction="horizontal" justify="between" align="start" gap="relaxed">
        <Stack gap="snug" minWidth="none">
          <Stack
            direction="horizontal"
            align="center"
            gap="snug"
            overflow={phone ? 'auto' : undefined}
            style={phone ? { scrollbarWidth: 'none', whiteSpace: 'nowrap' } : undefined}
          >
            {onSessions && (
              <Button size="icon-sm" variant="ghost" aria-label="Sessions" onClick={onSessions}>
                <Icon name="panel-left" size="2xs" label="" />
              </Button>
            )}
            <RunSourceChips sources={run.sources} max={1} />
            {branch && (
              <Stack direction="horizontal" align="center" gap="tight">
                <Icon name="git-branch" size="2xs" color="subtle" label="" />
                <Text size="supporting" color="muted" mono>
                  {branchName(branch.label)}
                </Text>
              </Stack>
            )}
          </Stack>
          {/* A phone keeps the title to two lines. */}
          <Stack
            style={
              phone
                ? {
                    display: '-webkit-box',
                    WebkitLineClamp: 2,
                    WebkitBoxOrient: 'vertical',
                    overflow: 'hidden',
                  }
                : undefined
            }
          >
            <Heading variant="section">{run.summary}</Heading>
          </Stack>
          <Stack direction="horizontal" align="center" gap="snug" style={{ paddingTop: 6 }}>
            {ended && canRun && (
              <Button size="sm" variant="outline" asChild>
                <Link href={APP_ROUTES.agents.detail(run.agent.slug)}>
                  <Icon name="play" size="2xs" label="" />
                  New run
                </Link>
              </Button>
            )}
            {!phone && run.traceUrl && (
              <Button size="sm" variant="ghost" asChild>
                <Link href={run.traceUrl} target="_blank" rel="noopener noreferrer">
                  <Icon name="activity" size="2xs" label="" />
                  Trace
                </Link>
              </Button>
            )}
            {onDetails && (
              <Button size="sm" variant="ghost" onClick={onDetails}>
                <Icon name="info" size="2xs" label="" />
                Details
              </Button>
            )}
            {phone && run.traceUrl && <MoreMenu traceUrl={run.traceUrl} />}
          </Stack>
        </Stack>
        {!narrow && (
          <Stack direction="horizontal" gap="loose" shrink style={{ paddingTop: 4 }}>
            <Figure label="Elapsed" value={formatSpan(run.durationMs)} />
            <Figure
              label="Spend"
              value={`$${run.spend.usd.toFixed(2)}`}
              sub={run.spend.limitUsd ? `/ $${run.spend.limitUsd.toFixed(2)}` : undefined}
            />
            {progress && <Figure label="Plan" value={`${progress.done}/${progress.total}`} />}
          </Stack>
        )}
      </Stack>
      {/* The run's state as a bar the width of the page: label, then progress through the plan. */}
      <Stack direction="horizontal" align="center" gap="cozy" style={{ paddingBottom: 14 }}>
        <Stack direction="horizontal" align="center" gap="snug" style={NOWRAP}>
          {status && (
            <>
              <Icon
                name={status.icon}
                spin={status.icon === 'loader'}
                size="2xs"
                color={status.tone === 'muted' ? 'subtle' : status.tone}
                label=""
              />
              <Text size="caption" weight="strong" color={status.tone}>
                {status.label}
              </Text>
            </>
          )}
          <Text size="caption" color="subtle">
            {narrow
              ? `${formatSpan(run.durationMs)} · $${run.spend.usd.toFixed(2)}`
              : `${run.agent.name} · started ${clockOf(run.startedAt)}`}
            {run.actingFor && ` · acts for ${run.actingFor}`}
          </Text>
        </Stack>
        <Stack grow style={{ visibility: (progress || ended) && !budget ? 'visible' : 'hidden' }}>
          <Progress
            label="Progress through the plan"
            value={progress ? (progress.done / progress.total) * 100 : 100}
            size="hairline"
          />
        </Stack>
      </Stack>
    </Stack>
  );
}
