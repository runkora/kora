'use client';

/**
 * @tier 3 — proposed, run-page-scoped. The run page as a three-pane workspace: sessions on the
 * left, the run read as a document in the middle, and the run's facts in a rail on the right.
 * Design-only; drawn from the ACP-shaped view model in `_lib/run-view-proposed`.
 */

import {
  Avatar,
  Button,
  Heading,
  Icon,
  Stack,
  StreamingCursor,
  Text,
  Textarea,
  TooltipProvider,
  type IconName,
} from '@runkora/hades';
import { useLayoutEffect, useRef, useState } from 'react';
import { StopIcon } from './input-area';
import { InstructionsMarkdown, RunSourceChips } from '@runkora/components';
import { RunRail } from './run-rail-proposed';
import {
  layoutFor,
  RunLayoutContext,
  useRunLayout,
  type RunLayout,
} from '../_lib/run-layout-proposed';
import { promptEvent, TriggerEventView } from './trigger-event-proposed';
import {
  allSteps,
  clockAt,
  formatSpan,
  quietSummary,
  rowsOf,
  type Entry,
  type Row,
  type RunView,
  type Step,
  type StepKind,
} from '../_lib/run-view-proposed';

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
  active,
  running,
  indent,
}: {
  label: string;
  meta: string;
  active?: boolean;
  running?: boolean;
  indent?: boolean;
}) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="snug"
      style={{
        padding: '7px 10px',
        paddingLeft: indent ? 28 : 10,
        borderRadius: 6,
        background: active ? 'var(--color-surface-elevated)' : 'transparent',
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
}

export interface EarlierRun {
  label: string;
  meta: string;
}

const DEFAULT_EARLIER: EarlierRun[] = [
  { label: 'Add idempotency keys to refunds', meta: '3h' },
  { label: 'Fix flaky settlement export test', meta: '1d' },
  { label: 'Bump the ledger client to 4.2', meta: '2d' },
];

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
      }}
    >
      <Stack direction="horizontal" align="center" gap="snug" style={{ padding: '0 10px' }}>
        <Icon name="arrow-left" size="2xs" color="subtle" label="" />
        <Text size="supporting" color="muted">
          {run.agent.name}
        </Text>
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
      <Stack gap="tight">
        <Text size="caption" color="subtle" style={{ padding: '0 10px 6px' }}>
          Earlier runs
        </Text>
        {earlier.map((r) => (
          <SessionRow key={r.label + r.meta} label={r.label} meta={r.meta} />
        ))}
      </Stack>
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
  if (!plan || plan.type !== 'plan') return null;
  return {
    done: plan.plan.items.filter((i) => i.status === 'done').length,
    total: plan.plan.items.length,
  };
}

function RunHeader({
  run,
  onSessions,
  onDetails,
}: {
  run: RunView;
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
  const running = run.state === 'active';
  const live = running || run.state === 'idle';
  const budget = run.state === 'budget_exhausted';
  const branch = run.sources.find((s) => s.kind === 'branch');
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
                  {branch.label.replace(/^branch /, '')} → main
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
            {!live && (
              <Button size="sm" variant="outline">
                <Icon name="play" size="2xs" label="" />
                New run
              </Button>
            )}
            {!phone && (
              <Button size="sm" variant="ghost">
                <Icon name="activity" size="2xs" label="" />
                Trace
              </Button>
            )}
            {onDetails && (
              <Button size="sm" variant="ghost" onClick={onDetails}>
                <Icon name="info" size="2xs" label="" />
                Details
              </Button>
            )}
            <Button size="icon-sm" variant="ghost" aria-label="More actions">
              <Icon name="more-horizontal" size="2xs" label="" />
            </Button>
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
            name={running ? 'loader' : budget ? 'circle-alert' : 'circle-check'}
            size="2xs"
            color={running ? 'accent' : budget ? 'status-error' : 'subtle'}
            label=""
          />
          <Text
            size="caption"
            weight="strong"
            color={running ? 'accent' : budget ? 'status-error' : 'muted'}
          >
            {running
              ? 'Working'
              : budget
                ? 'Budget spent'
                : run.state === 'completed'
                  ? 'Completed'
                  : 'Ended'}
          </Text>
          <Text size="caption" color="subtle">
            {narrow
              ? `${formatSpan(run.durationMs)} · $${run.spend.usd.toFixed(2)}`
              : `${run.agent.name} · started ${clockAt(run, 0).slice(0, 5)}`}
          </Text>
        </Stack>
        <Stack
          grow
          style={{
            height: 3,
            borderRadius: 2,
            background: 'var(--color-surface-elevated)',
            visibility: (progress || !live) && !budget ? 'visible' : 'hidden',
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

/** A person's or trigger's message: a soft shaded block, like a quoted prompt. */
function MessageBlock({ entry }: { entry: Extract<Entry, { type: 'message' }> }) {
  const trigger = entry.author.kind === 'trigger';
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
        {trigger ? (
          <Icon name="zap" size="2xs" color="subtle" label="" />
        ) : (
          <Avatar name={entry.author.name} size="2xs" />
        )}
        <Text size="caption" color="muted">
          {entry.author.name}
          {trigger ? ' trigger' : ''}
        </Text>
      </Stack>
      <Text size="body" style={{ lineHeight: 1.6, whiteSpace: 'pre-wrap' }}>
        {entry.text}
      </Text>
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

/** A command without the plumbing: a leading `cd` dropped and a chain split into its commands. */
function commandParts(step: Step): string[] {
  if (step.kind !== 'execute') return [step.target];
  const parts = step.target
    .split(/;\s*|&&\s*/)
    .map((p) => p.trim())
    .filter((p) => p && !/^cd\s+\S+$/.test(p));
  return parts.length ? parts : [step.target];
}

/** A tool call as a quiet block: bold verb, monospace target, then a fading preview of what came back. */
function CallBlock({ step, nested }: { step: Step; nested?: Step[] }) {
  const failed = step.status === 'failed' || step.status === 'denied';
  const running = step.status === 'running';
  const preview = step.errorLine ?? step.output?.split('\n').slice(0, 4).join('\n');
  const parts = commandParts(step);
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
        <Text size="supporting" weight="strong" style={NOWRAP}>
          {step.verb}
        </Text>
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
      </Stack>
      {step.diffs && step.diffs.length > 0 && (
        <Stack style={{ borderTop: HAIR }}>
          {step.diffs.map((d) => {
            const { name, dir } = shortName(d.path);
            return (
              <Stack
                key={d.path}
                direction="horizontal"
                align="center"
                gap="snug"
                style={{ padding: '7px 14px' }}
              >
                <Text size="supporting">{name}</Text>
                <Text size="caption" color="subtle" truncate style={{ flex: 1, minWidth: 0 }}>
                  {dir}
                </Text>
                <Text size="caption" mono color="status-healthy">
                  +{d.added}
                </Text>
                <Text size="caption" mono color="status-error">
                  −{d.removed}
                </Text>
              </Stack>
            );
          })}
        </Stack>
      )}
      {preview && !step.diffs?.length && (
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

/** Reads and searches the reader skims past: one muted line. */
function QuietLine({ steps }: { steps: Step[] }) {
  return (
    <Stack direction="horizontal" align="center" gap="snug" style={{ padding: '2px 2px' }}>
      <Icon name="file-text" size="2xs" color="subtle" label="" />
      <Text size="supporting" color="subtle">
        {quietSummary(steps)}
      </Text>
      <Icon name="chevron-right" size="2xs" color="subtle" label="" />
    </Stack>
  );
}

/** A trigger's message as its event: the adapter's summary when there is one, else from the prompt. */
function triggerEvent(e: Extract<Entry, { type: 'message' }>, time: string) {
  const base = { trigger: e.author.name, time, joined: e.joined, prompt: e.text };
  return e.event ? { ...e.event, ...base } : promptEvent({ ...base, via: e.author.via });
}

function RowView({ row, run }: { row: Row; run: RunView }) {
  if (row.type === 'quiet') return <QuietLine steps={row.steps} />;
  if (row.type === 'step') return <CallBlock step={row.step} nested={row.children} />;
  const e = row.entry;
  switch (e.type) {
    case 'message':
      return e.author.kind === 'trigger' ? (
        <TriggerEventView e={triggerEvent(e, clockAt(run, e.at).slice(0, 5))} />
      ) : (
        <MessageBlock entry={e} />
      );
    case 'text':
      return <Prose text={e.text} streaming={e.streaming} />;
    case 'plan':
      return null; // The plan lives in the rail and updates in place.
    case 'retry':
      return (
        <Text size="supporting" color="subtle">
          Retried {e.attempts} times over {e.seconds}s: {e.reason}
        </Text>
      );
    case 'question':
      return <Prose text={e.text} />;
    case 'stopped':
      return (
        <Text size="supporting" color="subtle">
          Stopped by {e.by}
        </Text>
      );
  }
}

const BOX = {
  border: LINE,
  borderRadius: 10,
  background: 'var(--color-surface)',
} as const;

/** The agent's step in progress, for the status strip: "Running gh api …". */
function currentStep(run: RunView): string | undefined {
  const s = allSteps(run).find((x) => x.status === 'running');
  if (!s) return undefined;
  return `${s.verb} ${commandParts(s)[0] ?? s.target}`;
}

/** Send and Stop share one square: 32px, the size of the composer's existing buttons. */
const ACTION = {
  width: 32,
  height: 32,
  borderRadius: 8,
  flexShrink: 0,
  padding: 0,
  border: 'none',
} as const;

function SendButton({ ready }: { ready: boolean }) {
  return (
    <Button
      disabled={!ready}
      aria-label="Send"
      style={{
        ...ACTION,
        background: ready ? 'var(--color-accent)' : 'var(--color-surface-overlay)',
        color: ready ? 'var(--color-accent-foreground)' : 'var(--color-subtle)',
      }}
    >
      <Icon name="arrow-up" size="xs" label="" />
    </Button>
  );
}

/** Stop ends the agent's turn: a solid button with a filled square, as chat products draw it. */
function StopButton() {
  return (
    <Button
      aria-label="Stop"
      style={{
        ...ACTION,
        background: 'var(--color-foreground)',
        color: 'var(--color-background)',
      }}
    >
      <StopIcon />
    </Button>
  );
}

function Field({
  value,
  onChange,
  placeholder,
  rows = 1,
  maxRows = 6,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  /** The floor: the box never shrinks below this many lines. */
  rows?: number;
  maxRows?: number;
}) {
  return (
    <Textarea
      variant="bare"
      autoResize
      maxRows={maxRows}
      rows={rows}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      style={{ fontSize: 14, lineHeight: 1.6, width: '100%', padding: 0, borderRadius: 0 }}
    />
  );
}

/** Messages sent while the agent works, waiting for its next step. */
function Pending({ run, inset }: { run: RunView; inset: number }) {
  return (
    <>
      {run.queued.map((q) => (
        <Stack
          key={q.id}
          direction="horizontal"
          align="center"
          gap="snug"
          style={{ padding: `0 ${inset}px` }}
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
    </>
  );
}

/**
 * The composer offers only what the API does: one message, read at the agent's next step while it
 * works, and Stop, which ends the turn. A strip on top says what the agent is doing now.
 */
function Composer({ run }: { run: RunView }) {
  const phone = useRunLayout() === 'phone';
  const [text, setText] = useState('');
  const ready = !!text.trim();
  const working = run.state === 'active';
  const name = run.agent.name;
  const now = currentStep(run);
  return (
    <Stack style={{ padding: phone ? '0 12px 12px' : '0 32px 22px' }}>
      <Stack style={{ maxWidth: READING_WIDTH, width: '100%', margin: '0 auto' }}>
        <Stack style={{ ...BOX, overflow: 'hidden' }}>
          {working && (
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
                {now ?? 'Thinking'}
              </Text>
            </Stack>
          )}
          {run.queued.length > 0 && (
            <Stack gap="tight" style={{ padding: '8px 0', borderBottom: LINE }}>
              <Pending run={run} inset={14} />
            </Stack>
          )}
          <Stack gap="snug" style={{ padding: '14px 10px 10px 14px' }}>
            <Field
              value={text}
              onChange={setText}
              rows={phone ? 2 : 3}
              maxRows={phone ? 8 : 14}
              placeholder={
                working ? `Message ${name}. This won't stop its work.` : `Reply to ${name}`
              }
            />
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption" color="subtle" style={{ flex: 1 }}>
                {working && !ready
                  ? 'Stop ends its current turn'
                  : phone
                    ? ''
                    : 'Shift+Enter for a new line'}
              </Text>
              {working && !ready ? <StopButton /> : <SendButton ready={ready} />}
            </Stack>
          </Stack>
        </Stack>
      </Stack>
    </Stack>
  );
}

// ── Page ─────────────────────────────────────────────────────────────────────

export function RunMixPage({
  run,
  earlier = DEFAULT_EARLIER,
  height = '100vh',
}: {
  run: RunView;
  earlier?: EarlierRun[];
  /** The page's height; a framed preview passes the device's. */
  height?: string;
}) {
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
  const live = run.state === 'active' || run.state === 'idle';
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
                onSessions={sessionsPane ? undefined : () => setSheet('sessions')}
                onDetails={railPane ? undefined : () => setSheet('details')}
              />
            </Stack>
            {/* The thread scrolls the full height; a live composer floats over its foot. */}
            <Stack gap="none" style={{ position: 'relative', flex: '1 1 0', minHeight: 0 }}>
              <Stack
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
                      run={run}
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
            {!live && <EndLine run={run} />}
          </Stack>
          {railPane && <RunRail run={run} />}
          {sheet && (
            <Sheet
              side={sheet === 'sessions' ? 'left' : layout === 'phone' ? 'bottom' : 'right'}
              onClose={() => setSheet(null)}
            >
              {sheet === 'sessions' ? (
                <SessionsPane run={run} earlier={earlier} drawer />
              ) : (
                <RunRail run={run} sheet />
              )}
            </Sheet>
          )}
        </Stack>
      </TooltipProvider>
    </RunLayoutContext.Provider>
  );
}

/**
 * A panel over the page: sessions from the left, details from the right on a tablet and from the
 * bottom on a phone. Positioned inside the page so a framed preview keeps it in its frame; the
 * real page would use a Hades sheet.
 */
function Sheet({
  side,
  onClose,
  children,
}: {
  side: 'left' | 'right' | 'bottom';
  onClose: () => void;
  children: React.ReactNode;
}) {
  const edge: React.CSSProperties =
    side === 'bottom'
      ? { left: 0, right: 0, bottom: 0, maxHeight: '80%', borderRadius: '14px 14px 0 0' }
      : { top: 0, bottom: 0, [side]: 0, width: 'min(320px, 86%)' };
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
          ...edge,
          zIndex: 11,
          background: 'var(--color-surface)',
          border: LINE,
          overflowY: 'auto',
        }}
      >
        {side === 'bottom' && (
          <Stack align="center" style={{ padding: '8px 0 4px' }}>
            <Stack
              style={{ width: 36, height: 4, borderRadius: 2, background: 'var(--color-border)' }}
            />
          </Stack>
        )}
        {children}
      </Stack>
    </>
  );
}

/** An ended run closes with one line instead of a composer; a spent budget says which and when it resets. */
function EndLine({ run }: { run: RunView }) {
  const phone = useRunLayout() === 'phone';
  const b = run.budgetStop;
  const pad = phone ? '12px 16px 16px' : '14px 32px 20px';
  if (run.state === 'budget_exhausted' && b) {
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
            Stopped: {b.owner}&apos;s {b.window} budget of ${b.limitUsd.toFixed(2)} is spent
          </Text>
          <Text size="caption" color="subtle">
            Model calls were refused after ${run.spend.usd.toFixed(2)}. The budget resets {b.resets}
            .
          </Text>
        </Stack>
        <Button size="sm" variant="outline">
          Raise budget
        </Button>
      </Stack>
    );
  }
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="snug"
      style={{ padding: pad, borderTop: LINE }}
    >
      <Icon name="circle-check" size="2xs" color="accent" label="" />
      <Text size="supporting" weight="emphasis">
        Completed
      </Text>
      <Text size="supporting" color="subtle">
        in {formatSpan(run.durationMs)} · ${run.spend.usd.toFixed(2)} ·{' '}
        {(run.spend.tokens / 1000).toFixed(1)}K tokens
      </Text>
    </Stack>
  );
}
