'use client';

/**
 * @tier 3 — run-page-scoped. The run page as a three-pane workspace: sessions on the left, the run
 * read as a document in the middle, and the run's facts in a rail on the right. Panes fold into a
 * drawer and a sheet as the page narrows.
 */

import Link from 'next/link';
import {
  Avatar,
  BottomSheet,
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  EmptyState,
  Heading,
  Icon,
  Popover,
  PopoverContent,
  PopoverTrigger,
  SidePanel,
  SidePanelHeader,
  Stack,
  StreamingCursor,
  Text,
  Textarea,
  TooltipProvider,
  type IconName,
} from '@runkora/hades';
import { useLayoutEffect, useRef, useState } from 'react';
import { InstructionsMarkdown, PermissionHint, RunSourceChips } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useHasScope } from '@runkora/app/(dashboard)/_components/permissions-provider';
import { useRunContext } from '../_context/run-context';
import { useNow } from '../_hooks/use-now';
import { RunRail } from './run-rail';
import { ConnectionBanner } from './connection-banner';
import { layoutFor, RunLayoutContext, useRunLayout, type RunLayout } from '../_lib/run-layout';
import { TriggerEventView, triggerEventOf } from './trigger-event';
import { earlierRunsOf, toRunView, type EarlierRun } from '../_lib/to-run-view';
import {
  allSteps,
  clockOf,
  commandParts,
  ENDED,
  formatSpan,
  quietSummary,
  rowsOf,
  type Entry,
  type Row,
  type RunView,
  type Step,
  type StepKind,
} from '../_lib/run-view';

// ── Shared pieces ────────────────────────────────────────────────────────────

const LINE = '1px solid var(--color-border-subtle)';
/** A hairline inside a tool block, fainter than the pane dividers. */
const HAIR = '1px solid color-mix(in oklch, var(--color-foreground) 6%, transparent)';
const NOWRAP: React.CSSProperties = { whiteSpace: 'nowrap', flexShrink: 0 };
const READING_WIDTH = 720;

const KIND_ICON: Record<StepKind, IconName> = {
  read: 'file-text',
  edit: 'edit',
  execute: 'square-terminal',
  search: 'search',
  fetch: 'globe',
  think: 'sparkles',
  mcp: 'network',
  subagent: 'bot',
  sandbox: 'box',
  other: 'wrench',
};

function shortName(path: string): { name: string; dir: string } {
  const i = path.lastIndexOf('/');
  return i < 0 ? { name: path, dir: '' } : { name: path.slice(i + 1), dir: path.slice(0, i) };
}

// ── Left: sessions ───────────────────────────────────────────────────────────

function SessionRow({
  label,
  meta,
  href,
  active,
  running,
  indent,
}: {
  label: string;
  meta: string;
  href?: string;
  active?: boolean;
  running?: boolean;
  indent?: boolean;
}) {
  const row = (
    <Stack
      direction="horizontal"
      align="center"
      gap="snug"
      style={{
        padding: '7px 10px',
        paddingLeft: indent ? 28 : 10,
        borderRadius: 6,
        background: active ? 'var(--color-surface-elevated)' : 'transparent',
        textDecoration: 'none',
        color: 'inherit',
      }}
    >
      <Icon
        name={running ? 'loader' : 'circle-check'}
        size="2xs"
        color={running ? 'accent' : 'subtle'}
        label=""
      />
      <Text
        size="supporting"
        color={active ? 'foreground' : 'muted'}
        truncate
        style={{ flex: 1, minWidth: 0 }}
      >
        {label}
      </Text>
      <Text size="caption" color="subtle" style={NOWRAP}>
        {meta}
      </Text>
    </Stack>
  );
  return href ? (
    <Stack asChild>
      <Link href={href} style={{ textDecoration: 'none', color: 'inherit' }}>
        {row}
      </Link>
    </Stack>
  ) : (
    row
  );
}

function SessionsPane({
  run,
  earlier,
  drawer,
}: {
  run: RunView;
  earlier: EarlierRun[];
  /** Fills the drawer it opens in instead of standing beside the thread. */
  drawer?: boolean;
}) {
  const subagents = allSteps(run).filter((s) => s.kind === 'subagent');
  return (
    <Stack
      gap="relaxed"
      style={{
        width: drawer ? '100%' : 264,
        flexShrink: 0,
        borderRight: drawer ? undefined : LINE,
        padding: '18px 12px',
        overflowY: 'auto',
      }}
    >
      <Stack asChild direction="horizontal" align="center" gap="snug">
        <Link
          href={APP_ROUTES.agents.detail(run.agent.slug)}
          style={{ padding: '0 10px', textDecoration: 'none', color: 'inherit' }}
        >
          <Icon name="arrow-left" size="2xs" color="subtle" label="" />
          <Text size="supporting" color="muted">
            {run.agent.name}
          </Text>
        </Link>
      </Stack>
      <Stack gap="tight">
        <Text size="caption" color="subtle" style={{ padding: '0 10px 6px' }}>
          This run
        </Text>
        <SessionRow
          label="Main session"
          meta={formatSpan(run.durationMs)}
          active
          running={run.state === 'active'}
        />
        {subagents.map((s) => (
          <SessionRow
            key={s.id}
            label={commandParts(s)[0]}
            meta={s.status === 'running' ? 'now' : formatSpan(s.durationMs ?? 0)}
            running={s.status === 'running'}
            indent
          />
        ))}
      </Stack>
      {earlier.length > 0 && (
        <Stack gap="tight">
          <Text size="caption" color="subtle" style={{ padding: '0 10px 6px' }}>
            Other runs
          </Text>
          {earlier.map((r) => (
            <SessionRow
              key={r.id}
              label={r.label}
              meta={r.meta}
              running={r.running}
              href={APP_ROUTES.agents.run(run.agent.slug, r.id)}
            />
          ))}
        </Stack>
      )}
    </Stack>
  );
}

// ── Centre: header ───────────────────────────────────────────────────────────

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

/** The run's state in a word or two, with the icon and colour the header and end line share. */
function stateLabel(run: RunView): {
  label: string;
  icon: IconName;
  tone: 'accent' | 'status-error' | 'muted';
} {
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

/** The "…" menu. Trace lives here on every width; a phone has no room for its button. */
function MoreMenu({ traceUrl }: { traceUrl: string }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="icon-sm" variant="ghost" aria-label="More actions">
          <Icon name="more-horizontal" size="2xs" label="" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start">
        <Stack gap="none" style={{ minWidth: 160 }}>
          <Button asChild size="sm" variant="ghost" style={{ justifyContent: 'flex-start' }}>
            <Link href={traceUrl} target="_blank" rel="noopener noreferrer">
              <Icon name="activity" size="2xs" label="" />
              Trace
            </Link>
          </Button>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}

function RunHeader({
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
  const pct = progress ? (progress.done / progress.total) * 100 : 100;
  const ended = ENDED.has(run.state);
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
            style={
              phone
                ? { overflowX: 'auto', scrollbarWidth: 'none', whiteSpace: 'nowrap' }
                : undefined
            }
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
                  {branch.label.replace(/^branch /, '')}
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
            {run.traceUrl && <MoreMenu traceUrl={run.traceUrl} />}
          </Stack>
        </Stack>
        <Stack
          direction="horizontal"
          gap="loose"
          style={{ paddingTop: 4, flexShrink: 0, display: narrow ? 'none' : undefined }}
        >
          <Figure label="Elapsed" value={formatSpan(run.durationMs)} />
          <Figure
            label="Spend"
            value={`$${run.spend.usd.toFixed(2)}`}
            sub={run.spend.limitUsd ? `/ $${run.spend.limitUsd.toFixed(0)}` : undefined}
          />
          {progress && <Figure label="Plan" value={`${progress.done}/${progress.total}`} />}
        </Stack>
      </Stack>
      {/* The run's state as a bar the width of the page: label, then progress through the plan. */}
      <Stack direction="horizontal" align="center" gap="cozy" style={{ paddingBottom: 14 }}>
        <Stack direction="horizontal" align="center" gap="snug" style={NOWRAP}>
          <Icon
            name={status.icon}
            size="2xs"
            color={status.tone === 'muted' ? 'subtle' : status.tone}
            label=""
          />
          <Text size="caption" weight="strong" color={status.tone}>
            {status.label}
          </Text>
          <Text size="caption" color="subtle">
            {narrow
              ? `${formatSpan(run.durationMs)} · $${run.spend.usd.toFixed(2)}`
              : `${run.agent.name} · started ${clockOf(run.startedAt)}`}
            {run.actingFor && ` · acts for ${run.actingFor}`}
          </Text>
        </Stack>
        <Stack
          grow
          style={{
            height: 3,
            borderRadius: 2,
            background: 'var(--color-surface-elevated)',
            visibility: (progress || ended) && !budget ? 'visible' : 'hidden',
          }}
        >
          <Stack
            style={{
              width: `${pct}%`,
              height: 3,
              borderRadius: 2,
              background: 'var(--color-accent)',
            }}
          />
        </Stack>
      </Stack>
    </Stack>
  );
}

// ── Centre: the run as a document ────────────────────────────────────────────

/** A person's message: a soft shaded block with the sender's name. */
function MessageBlock({ entry }: { entry: Extract<Entry, { type: 'message' }> }) {
  return (
    <Stack
      gap="snug"
      style={{
        padding: '14px 18px',
        borderRadius: 10,
        background: 'var(--color-surface-elevated)',
      }}
    >
      <Stack direction="horizontal" align="center" gap="snug">
        <Avatar name={entry.author.name} size="2xs" />
        <Text size="caption" color="muted" style={{ flex: 1 }}>
          {entry.author.name}
        </Text>
        <Text size="caption" color="subtle">
          {entry.time}
        </Text>
      </Stack>
      <Text size="body" style={{ lineHeight: 1.6, whiteSpace: 'pre-wrap' }}>
        {entry.text}
      </Text>
      {entry.error && (
        <Text size="caption" color="status-error" role="alert">
          {entry.error}
        </Text>
      )}
    </Stack>
  );
}

/** The agent's own words, set for reading. */
function Prose({ text, streaming }: { text: string; streaming?: boolean }) {
  return (
    <Stack style={{ fontSize: 16, lineHeight: 1.7 }}>
      <InstructionsMarkdown content={text} />
      {streaming && <StreamingCursor />}
    </Stack>
  );
}

/** A tool call as a quiet block: bold verb, monospace target, then a fading preview of what came back. */
function CallBlock({ step, nested }: { step: Step; nested?: Step[] }) {
  const failed = step.status === 'failed' || step.status === 'denied';
  const running = step.status === 'running';
  const preview =
    step.errorLine ?? step.policy?.reason ?? step.output?.split('\n').slice(0, 4).join('\n');
  const parts = commandParts(step);
  // The block previews; opening it shows the whole command, output and diff.
  const [open, setOpen] = useState(false);
  const full =
    (parts.length > 1 || !!step.output || !!step.diffs?.some((d) => d.lines?.length)) && !running;
  return (
    <Stack
      style={{
        border: `1px solid ${failed ? 'color-mix(in oklch, var(--color-status-error) 30%, transparent)' : 'color-mix(in oklch, var(--color-foreground) 8%, transparent)'}`,
        background: 'color-mix(in oklch, var(--color-foreground) 2.5%, transparent)',
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      <Stack direction="horizontal" align="center" gap="snug" style={{ padding: '9px 14px' }}>
        <Icon
          name={running ? 'loader' : KIND_ICON[step.kind]}
          size="2xs"
          color={failed ? 'status-error' : running ? 'accent' : 'subtle'}
          label=""
        />
        {step.verb && (
          <Text size="supporting" weight="strong" style={NOWRAP}>
            {step.verb}
          </Text>
        )}
        <Text size="supporting" mono color="muted" truncate style={{ flex: 1, minWidth: 0 }}>
          {parts[0]}
        </Text>
        {parts.length > 1 && (
          <Text size="caption" color="subtle" style={NOWRAP}>
            +{parts.length - 1} more
          </Text>
        )}
        {step.exitCode !== undefined && step.exitCode !== 0 && (
          <Text size="caption" mono color="status-error" style={NOWRAP}>
            exit {step.exitCode}
          </Text>
        )}
        <Text size="caption" color="subtle" tabular style={NOWRAP}>
          {running ? 'now' : step.durationMs !== null ? formatSpan(step.durationMs) : ''}
        </Text>
        {full && (
          <Button
            size="icon-sm"
            variant="ghost"
            aria-expanded={open}
            aria-label={open ? 'Hide the full call' : 'Show the full call'}
            onClick={() => setOpen(!open)}
            style={{ width: 20, height: 20 }}
          >
            <Icon name={open ? 'chevron-down' : 'chevron-right'} size="2xs" label="" />
          </Button>
        )}
      </Stack>
      {open && <CallDetail step={step} />}
      {!open && step.diffs && step.diffs.length > 0 && (
        <Stack style={{ borderTop: HAIR }}>
          {step.diffs.map((d, i) => {
            const { name, dir } = shortName(d.path);
            return (
              <Stack
                key={`${d.path}-${i}`}
                direction="horizontal"
                align="center"
                gap="snug"
                style={{ padding: '7px 14px' }}
              >
                <Text size="supporting">{name}</Text>
                <Text size="caption" color="subtle" truncate style={{ flex: 1, minWidth: 0 }}>
                  {dir}
                </Text>
                {d.lines !== null && (
                  <>
                    <Text size="caption" mono color="status-healthy">
                      +{d.added}
                    </Text>
                    <Text size="caption" mono color="status-error">
                      −{d.removed}
                    </Text>
                  </>
                )}
              </Stack>
            );
          })}
        </Stack>
      )}
      {!open && preview && !step.diffs?.length && (
        <Stack
          style={{
            borderTop: HAIR,
            padding: '8px 14px 10px',
            maxHeight: 92,
            overflow: 'hidden',
            maskImage: 'linear-gradient(to bottom, black 55%, transparent)',
          }}
        >
          <Text
            size="caption"
            mono
            color={failed ? 'status-error' : 'subtle'}
            style={{ whiteSpace: 'pre-wrap', lineHeight: 1.6 }}
          >
            {preview}
          </Text>
        </Stack>
      )}
      {nested && nested.length > 0 && (
        <Stack style={{ borderTop: HAIR, padding: '8px 14px 10px' }} gap="tight">
          {nested.map((c) => (
            <Stack key={c.id} direction="horizontal" align="center" gap="snug">
              <Icon
                name={c.status === 'running' ? 'loader' : KIND_ICON[c.kind]}
                size="2xs"
                color={c.status === 'running' ? 'accent' : 'subtle'}
                label=""
              />
              <Text size="caption" color="muted" style={NOWRAP}>
                {c.verb}
              </Text>
              <Text size="caption" mono color="subtle" truncate>
                {c.target}
              </Text>
            </Stack>
          ))}
        </Stack>
      )}
    </Stack>
  );
}

/** Everything a call recorded: the whole command, its full output and each file's diff lines. */
function CallDetail({ step }: { step: Step }) {
  const mono = { whiteSpace: 'pre-wrap', lineHeight: 1.6, wordBreak: 'break-word' } as const;
  return (
    <Stack
      gap="snug"
      style={{ borderTop: HAIR, padding: '8px 14px 10px', maxHeight: 480, overflowY: 'auto' }}
    >
      {step.command && (
        <Text size="caption" mono style={mono}>
          {step.command}
        </Text>
      )}
      {step.diffs?.map((d, i) => (
        <Stack key={`${d.path}-${i}`} gap="none">
          <Text size="caption" weight="strong">
            {d.path}
          </Text>
          {d.lines?.map((l, k) => (
            <Text
              key={k}
              size="caption"
              mono
              color={
                l.kind === 'add' ? 'status-healthy' : l.kind === 'del' ? 'status-error' : 'subtle'
              }
              style={mono}
            >
              {l.kind === 'add' ? '+ ' : l.kind === 'del' ? '− ' : '  '}
              {l.text}
            </Text>
          ))}
        </Stack>
      ))}
      {step.output && (
        <Text
          size="caption"
          mono
          color={step.status === 'failed' ? 'status-error' : 'subtle'}
          style={mono}
        >
          {step.output}
        </Text>
      )}
    </Stack>
  );
}

/** Reads and searches the reader skims past: one muted line that opens to the calls. */
function QuietLine({ steps }: { steps: Step[] }) {
  const [open, setOpen] = useState(false);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <Button size="sm" variant="ghost" style={{ paddingInline: 2, height: 24 }}>
          <Icon name="file-text" size="2xs" color="subtle" label="" />
          <Text as="span" size="supporting" color="subtle">
            {quietSummary(steps)}
          </Text>
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size="2xs" color="subtle" label="" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <Stack gap="snug" style={{ paddingTop: 8 }}>
          {steps.map((s) => (
            <CallBlock key={s.id} step={s} />
          ))}
        </Stack>
      </CollapsibleContent>
    </Collapsible>
  );
}

function RowView({ row }: { row: Row }) {
  if (row.type === 'quiet') return <QuietLine steps={row.steps} />;
  if (row.type === 'step') return <CallBlock step={row.step} nested={row.children} />;
  const e = row.entry;
  switch (e.type) {
    case 'message':
      return e.author.kind === 'trigger' ? (
        <TriggerEventView e={triggerEventOf(e)} />
      ) : (
        <MessageBlock entry={e} />
      );
    case 'text':
      return <Prose text={e.text} streaming={e.streaming} />;
    case 'plan':
      return null; // The plan's progress shows in the header and updates in place.
    case 'notice':
      return (
        <Stack gap="none">
          <Text size="supporting" color={e.tone === 'error' ? 'status-error' : 'subtle'}>
            {e.text}
          </Text>
          {e.detail && (
            <Text size="caption" color="subtle" mono>
              {e.detail}
            </Text>
          )}
        </Stack>
      );
  }
}

const BOX = {
  border: LINE,
  borderRadius: 10,
  background: 'var(--color-surface)',
} as const;

/** Send and Stop share one square: 32px, the size of the composer's existing buttons. */
const ACTION = {
  width: 32,
  height: 32,
  borderRadius: 8,
  flexShrink: 0,
  padding: 0,
  border: 'none',
} as const;

function StopIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
      <rect x="4" y="4" width="16" height="16" rx="2" />
    </svg>
  );
}

/** Messages sent while the agent works, waiting for its next step. */
function Pending({ run }: { run: RunView }) {
  return (
    <Stack gap="tight" style={{ padding: '8px 0', borderBottom: LINE }}>
      {run.queued.map((q) => (
        <Stack
          key={q.id}
          direction="horizontal"
          align="center"
          gap="snug"
          style={{ padding: '0 14px' }}
        >
          <Icon name="clock" size="2xs" color="subtle" label="" />
          <Text size="supporting" color="muted" truncate style={{ flex: 1, minWidth: 0 }}>
            {q.text}
          </Text>
          <Text size="caption" color="subtle" style={NOWRAP}>
            Next step
          </Text>
        </Stack>
      ))}
    </Stack>
  );
}

/**
 * The composer offers only what the API does: one message, read at the agent's next step while it
 * works, and Stop, which ends the turn. A strip on top says what the agent is doing now.
 */
function Composer({ run }: { run: RunView }) {
  const { addHumanMessage, interruptRun, canSteer, isStarting, isPending, stopError } =
    useRunContext();
  const phone = useRunLayout() === 'phone';
  const [text, setText] = useState('');
  const allowed = canSteer !== false;
  const ready = !!text.trim() && allowed && !isStarting;
  const working = run.state === 'active';
  const name = run.agent.name;
  const send = () => {
    if (!ready) return;
    addHumanMessage(text.trim());
    setText('');
  };
  return (
    <Stack style={{ padding: phone ? '0 12px 12px' : '0 32px 22px' }}>
      <Stack style={{ maxWidth: READING_WIDTH, width: '100%', margin: '0 auto' }}>
        <Stack style={{ ...BOX, overflow: 'hidden' }}>
          {(working || isStarting || isPending) && (
            <Stack
              direction="horizontal"
              align="center"
              gap="snug"
              style={{
                padding: '7px 10px 7px 14px',
                borderBottom: LINE,
                background: 'var(--color-surface-elevated)',
              }}
            >
              <Icon name="loader" size="2xs" color="accent" label="" />
              <Text size="caption" color="muted" truncate style={{ flex: 1, minWidth: 0 }}>
                {run.activity ?? (isStarting ? 'Starting' : isPending ? 'Sending' : 'Thinking')}
              </Text>
            </Stack>
          )}
          {run.queued.length > 0 && <Pending run={run} />}
          <Stack gap="snug" style={{ padding: '14px 10px 10px 14px' }}>
            <Textarea
              variant="bare"
              autoResize
              rows={phone ? 2 : 3}
              maxRows={phone ? 8 : 14}
              value={text}
              disabled={!allowed}
              aria-label={`Message ${name}`}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  send();
                }
              }}
              placeholder={
                !allowed
                  ? 'You cannot message this run'
                  : isStarting
                    ? `${name} is starting`
                    : working
                      ? `Message ${name}. This won't stop its work.`
                      : `Reply to ${name}`
              }
              style={{ fontSize: 14, lineHeight: 1.6, width: '100%', padding: 0, borderRadius: 0 }}
            />
            <Stack direction="horizontal" align="center" gap="snug">
              <Text
                size="caption"
                color={stopError ? 'status-error' : 'subtle'}
                style={{ flex: 1 }}
              >
                {stopError ??
                  (working && !text.trim()
                    ? 'Stop ends its current turn'
                    : phone
                      ? ''
                      : 'Shift+Enter for a new line')}
              </Text>
              {working && !text.trim() ? (
                <PermissionHint allowed={allowed} action="stop this run">
                  <Button
                    aria-label="Stop"
                    disabled={!allowed}
                    onClick={interruptRun}
                    style={{
                      ...ACTION,
                      background: 'var(--color-foreground)',
                      color: 'var(--color-background)',
                    }}
                  >
                    <StopIcon />
                  </Button>
                </PermissionHint>
              ) : (
                <Button
                  disabled={!ready}
                  aria-label="Send"
                  onClick={send}
                  style={{
                    ...ACTION,
                    background: ready ? 'var(--color-accent)' : 'var(--color-surface-overlay)',
                    color: ready ? 'var(--color-accent-foreground)' : 'var(--color-subtle)',
                  }}
                >
                  <Icon name="arrow-up" size="xs" label="" />
                </Button>
              )}
            </Stack>
          </Stack>
        </Stack>
      </Stack>
    </Stack>
  );
}

// ── Page ─────────────────────────────────────────────────────────────────────

/** The run page, fed by the run context. `height` is the page's; a framed preview passes its device's. */
export function RunPage({ height = '100dvh' }: { height?: string }) {
  const ctx = useRunContext();
  const live = !ENDED.has(ctx.runState ?? 'pending');
  const now = useNow(live);
  const canRaiseBudget = useHasScope('platform:budgets:write');
  const run = toRunView(ctx, now);
  const earlier = earlierRunsOf(ctx.earlierRuns);
  const rows = rowsOf(run.entries);
  // Layout follows the page's own width, so a framed preview lays out like its device.
  const rootRef = useRef<HTMLDivElement>(null);
  const [layout, setLayout] = useState<RunLayout>('wide');
  useLayoutEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setLayout(layoutFor(el.offsetWidth)));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  const [sheet, setSheet] = useState<'sessions' | 'details' | null>(null);
  const sessionsPane = layout === 'wide';
  const railPane = layout === 'wide' || layout === 'laptop';
  // The spacer under the thread tracks the floating composer, so the last entry stops just above it.
  const composerRef = useRef<HTMLDivElement>(null);
  const [composerHeight, setComposerHeight] = useState(0);
  useLayoutEffect(() => {
    const el = composerRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setComposerHeight(el.offsetHeight));
    observer.observe(el);
    return () => observer.disconnect();
  }, [live]);
  // The thread follows new content while the reader is at its foot, and stays put once they scroll up.
  const threadRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);
  useLayoutEffect(() => {
    const el = threadRef.current;
    if (!el) return;
    const onScroll = () => {
      followRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, []);
  const last = run.entries.at(-1);
  const tail = `${run.entries.length}:${last?.type === 'text' ? last.text.length : ''}`;
  useLayoutEffect(() => {
    const el = threadRef.current;
    if (el && followRef.current) el.scrollTop = el.scrollHeight;
  }, [tail, composerHeight]);

  if (ctx.loadError) {
    return (
      <Stack grow align="center" justify="center" padding="relaxed" style={{ height }}>
        <EmptyState
          icon="lock"
          title="Run unavailable"
          description={ctx.loadError}
          action={
            <Button variant="outline" size="sm" asChild>
              <Link href={APP_ROUTES.agents.list}>Back to agents</Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  return (
    <RunLayoutContext.Provider value={layout}>
      <TooltipProvider>
        <Stack
          ref={rootRef}
          direction="horizontal"
          style={{
            height,
            background: 'var(--color-background)',
            overflow: 'hidden',
            position: 'relative',
          }}
        >
          {sessionsPane && <SessionsPane run={run} earlier={earlier} />}
          <Stack grow minWidth="none" gap="none" style={{ overflow: 'hidden', height }}>
            <Stack style={{ borderBottom: LINE }}>
              <RunHeader
                run={run}
                canRun={!!ctx.agentType?.can_run}
                onSessions={sessionsPane ? undefined : () => setSheet('sessions')}
                onDetails={railPane ? undefined : () => setSheet('details')}
              />
            </Stack>
            <ConnectionBanner />
            {/* The thread scrolls the full height; a live composer floats over its foot. */}
            <Stack gap="none" style={{ position: 'relative', flex: '1 1 0', minHeight: 0 }}>
              <Stack
                ref={threadRef}
                style={{
                  flex: '1 1 0',
                  minHeight: 0,
                  overflowY: 'auto',
                  scrollbarWidth: 'none',
                  padding: layout === 'phone' ? '20px 16px 0' : '28px 32px 0',
                }}
              >
                <Stack
                  gap="relaxed"
                  style={{ maxWidth: READING_WIDTH, width: '100%', margin: '0 auto' }}
                >
                  {rows.map((row) => (
                    <RowView
                      key={
                        row.type === 'entry'
                          ? row.entry.id
                          : row.type === 'step'
                            ? row.step.id
                            : row.id
                      }
                      row={row}
                    />
                  ))}
                </Stack>
                {/* Room to scroll the last entry above the composer. Safari drops a scroller's bottom padding. */}
                <Stack style={{ flexShrink: 0, height: live ? composerHeight : 28 }} />
              </Stack>
              {live && (
                <Stack
                  ref={composerRef}
                  style={{
                    position: 'absolute',
                    left: 0,
                    right: 0,
                    bottom: 0,
                    paddingTop: 32,
                    background: 'linear-gradient(to top, var(--color-background) 60%, transparent)',
                  }}
                >
                  <Composer run={run} />
                </Stack>
              )}
            </Stack>
            {!live && <EndLine run={run} canRaiseBudget={canRaiseBudget} />}
          </Stack>
          {railPane && <RunRail run={run} />}
          {sheet === 'sessions' && (
            <SessionsDrawer onClose={() => setSheet(null)}>
              <SessionsPane run={run} earlier={earlier} drawer />
            </SessionsDrawer>
          )}
          {layout === 'phone' ? (
            <BottomSheet
              open={sheet === 'details'}
              onOpenChange={(open) => !open && setSheet(null)}
              title="Run details"
            >
              <RunRail run={run} sheet />
            </BottomSheet>
          ) : (
            <SidePanel open={sheet === 'details'} onClose={() => setSheet(null)} width={340}>
              <SidePanelHeader title="Run details" />
              <RunRail run={run} sheet />
            </SidePanel>
          )}
        </Stack>
      </TooltipProvider>
    </RunLayoutContext.Provider>
  );
}

/**
 * The sessions list over the page from the left edge. Hades has a right-edge SidePanel and a
 * BottomSheet but no left-edge drawer, so this one is drawn in the page.
 */
function SessionsDrawer({ onClose, children }: { onClose: () => void; children: React.ReactNode }) {
  return (
    <>
      <Button
        variant="ghost"
        aria-label="Close"
        onClick={onClose}
        style={{
          position: 'absolute',
          inset: 0,
          width: '100%',
          height: '100%',
          borderRadius: 0,
          background: 'color-mix(in oklch, var(--color-background) 60%, transparent)',
          zIndex: 10,
        }}
      />
      <Stack
        style={{
          position: 'absolute',
          top: 0,
          bottom: 0,
          left: 0,
          width: 'min(320px, 86%)',
          zIndex: 11,
          background: 'var(--color-surface)',
          border: LINE,
          overflowY: 'auto',
        }}
      >
        {children}
      </Stack>
    </>
  );
}

/** An ended run closes with one line instead of a composer; a spent budget says which and when it resets. */
function EndLine({ run, canRaiseBudget }: { run: RunView; canRaiseBudget: boolean }) {
  const phone = useRunLayout() === 'phone';
  const b = run.budgetStop;
  const pad = phone ? '12px 16px 16px' : '14px 32px 20px';
  if (run.state === 'budget_exhausted') {
    return (
      <Stack
        direction="horizontal"
        align="center"
        gap="cozy"
        wrap
        style={{ padding: pad, borderTop: LINE }}
      >
        <Icon name="circle-alert" size="2xs" color="status-error" label="" />
        <Stack grow minWidth="none" gap="none">
          <Text size="supporting" weight="emphasis">
            {b
              ? `Stopped: ${b.owner}'s ${b.window} budget of $${b.limitUsd.toFixed(2)} is spent`
              : 'Stopped: the budget is spent'}
          </Text>
          <Text size="caption" color="subtle">
            Model calls were refused after ${run.spend.usd.toFixed(2)}.
            {b?.resets ? ` The budget resets ${b.resets}.` : ''}
          </Text>
        </Stack>
        {canRaiseBudget && (
          <Button size="sm" variant="outline" asChild>
            <Link href={APP_ROUTES.modelGateway.budgets}>Raise budget</Link>
          </Button>
        )}
      </Stack>
    );
  }
  const status = stateLabel(run);
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="snug"
      style={{ padding: pad, borderTop: LINE }}
    >
      <Icon
        name={status.icon}
        size="2xs"
        color={status.tone === 'status-error' ? 'status-error' : 'accent'}
        label=""
      />
      <Text size="supporting" weight="emphasis">
        {status.label}
      </Text>
      <Text size="supporting" color="subtle">
        in {formatSpan(run.durationMs)} · ${run.spend.usd.toFixed(2)} ·{' '}
        {(run.spend.tokens / 1000).toFixed(1)}K tokens
      </Text>
    </Stack>
  );
}
