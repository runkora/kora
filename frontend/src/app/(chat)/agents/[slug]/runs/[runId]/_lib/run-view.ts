/**
 * The run page's view of one run. Tool calls take the shape ACP gives them (kind, title, content
 * and the harness's tool name), and `stepOf` turns any harness's call into one step type, so the
 * page renders Claude Code, Codex and OpenCode runs the same way. `toRunView` builds it.
 */
import type { IconName } from '@runkora/hades';
import type { RunSource, TriggerKind } from '@runkora/shared/api';
import { FRAMEWORK_LABELS } from '@runkora/shared/agent-frameworks';
import type { RunState } from '../_types';

/** The harness's display name: "Claude Code", or the raw id for one the page does not know. */
export const harnessLabel = (harness: string): string => FRAMEWORK_LABELS[harness] ?? harness;

/** ACP `tool_call.kind`. */
export type AcpKind =
  | 'read'
  | 'edit'
  | 'delete'
  | 'move'
  | 'search'
  | 'execute'
  | 'think'
  | 'fetch'
  | 'switch_mode'
  | 'other';

/** What policy decided for a call the edge checked. */
interface PolicyDecision {
  allowed: boolean;
  /** The scope the call needed, when known. */
  scope: string;
  /** The grant that allowed it, or what was refused. */
  reason: string;
}

/** A tool call in ACP's shape, with the times and policy decision the platform adds. */
export interface AcpToolCall {
  id: string;
  kind: AcpKind;
  title: string;
  status: 'in_progress' | 'completed' | 'failed';
  rawInput?: Record<string, unknown>;
  /** Text content, or the command output for an execute call. */
  output?: string;
  exitCode?: number;
  /** ACP diff content: Claude Code and OpenCode send the text. */
  diffs?: { path: string; oldText: string | null; newText: string }[];
  /** Codex sends a file change's path and kind without its text. */
  changes?: { path: string; kind: 'add' | 'update' | 'delete' }[];
  meta?: {
    /** The harness's own tool name, such as `Bash` or `mcp__github__get_pull_request`. */
    toolName?: string;
    /** Set on a call made inside a subagent. */
    parentId?: string;
  };
  /** The recorded result was cut; `originalBytes` is its full size. */
  truncated?: { originalBytes?: number };
  /** Server times (ms) of the call and its result. */
  startedAt?: number;
  endedAt?: number;
  policy?: PolicyDecision;
}

export type StepKind =
  | 'read'
  | 'edit'
  | 'execute'
  | 'search'
  | 'fetch'
  | 'think'
  | 'mcp'
  | 'subagent'
  | 'sandbox'
  | 'other';

interface DiffLine {
  kind: 'add' | 'del' | 'ctx';
  text: string;
}

interface FileDiff {
  path: string;
  added: number;
  removed: number;
  /** Null when the harness recorded the change without its text. */
  lines: DiffLine[] | null;
}

/** One call, ready to draw: what it was, its one-line summary and the detail behind it. */
export interface Step {
  id: string;
  kind: StepKind;
  verb: string;
  /** The object of the verb: a path, a command, a pattern, an MCP tool. */
  target: string;
  status: 'running' | 'done' | 'failed' | 'denied';
  durationMs: number | null;
  command?: string;
  output?: string;
  exitCode?: number;
  /** The first line of a failure that says why, skipping an `Exit code N` header. */
  errorLine?: string;
  diffs?: FileDiff[];
  mcp?: { server: string; tool: string; args: Record<string, unknown> };
  policy?: PolicyDecision;
  truncated?: AcpToolCall['truncated'];
  call: AcpToolCall;
}

export interface Plan {
  items: { text: string; status: 'done' | 'active' | 'pending' }[];
}

/** Who did what to which object, and a preview of that object, for a trigger's message. */
export interface TriggerEventSummary {
  /** Who caused it: a person, a bot such as GitHub Actions, or the platform itself. */
  actor: { name: string; kind: 'person' | 'bot' | 'system'; icon?: IconName; url?: string };
  /** "commented on", "opened", "failed on". */
  verb: string;
  object: { label: string; url?: string };
  /** Words a person wrote, such as a comment or an issue body. */
  quote?: string;
  /** The object as a link preview: its title and a few facts. */
  unfurl?: {
    icon: IconName;
    title: string;
    meta: string[];
    /** Lines added and removed, shown in the diff colours after the facts. */
    diff?: { added: number; removed: number };
    status?: { label: string; tone?: 'ok' | 'bad' };
  };
}

/** How a trigger's message reached the run. */
export interface TriggerOrigin {
  via?: TriggerKind;
  provider?: RunSource['provider'];
}

export type Entry =
  | {
      type: 'message';
      id: string;
      /** Local time the message arrived, as "09:14". */
      time: string;
      /** `unrecorded` marks a person's message from before senders were recorded; it has no name. */
      author: {
        kind: 'person' | 'agent' | 'trigger';
        name: string;
        unrecorded?: boolean;
      } & TriggerOrigin;
      text: string;
      /** A trigger_message: a trigger that sent this to a run already working. */
      joined?: boolean;
      /** The event behind a trigger's message, as its adapter recorded it. */
      event?: TriggerEventSummary;
      /** Why sending it from this page failed. */
      error?: string;
    }
  | { type: 'text'; id: string; text: string; streaming?: boolean }
  | { type: 'plan'; id: string; plan: Plan }
  | { type: 'call'; id: string; call: AcpToolCall }
  | { type: 'notice'; id: string; text: string; tone?: 'error'; detail?: string };

export interface QueuedMessage {
  id: string;
  text: string;
}

export interface RunView {
  id: string;
  summary: string;
  agent: { name: string; slug: string };
  /** The agent's harness id, such as `claude-code`; empty until known. */
  harness: string;
  model: string;
  /** Absent until the stream or the run record reports one. */
  state?: RunState;
  /** ISO time the run started, and when an ended run ended. */
  startedAt: string;
  endedAt?: string;
  /** Elapsed time; the page sets it from the clock for a live run. */
  durationMs: number;
  sources: RunSource[];
  /** The person a delegated run acts for, with their access. */
  actingFor?: string;
  spend: { usd: number; limitUsd: number | null };
  traceUrl: string | null;
  entries: Entry[];
  queued: QueuedMessage[];
  /** What the agent is doing now, for the composer's status strip. */
  activity?: string;
  /** What the platform recorded about the run, beside the conversation. */
  record?: RunRecord;
  /** The budget that stopped a budget_exhausted run: whose, its limit and its reset. */
  budgetStop?: { whose: string; limitUsd: number; resets: string };
}

/** Facts the API records: run usage, the sandbox the run started, the agent's connectors. */
interface RunRecord {
  requests: number;
  /** Uncached input, output, and cached input (read and written) tokens. */
  inputTokens: number;
  outputTokens: number;
  cachedTokens: number;
  sandbox?: { image: string; environment: string };
  connectors: { connection: string; actions: string[] }[];
}

// ── Steps from ACP calls ─────────────────────────────────────────────────────

const WORKSPACE = /^\/workspaces\/[^/]+\//;

/** A path relative to the run's workspace, which every call repeats otherwise. */
function shortPath(path: string): string {
  return path.replace(WORKSPACE, '');
}

/** The first line of a multi-line value, with an ellipsis when more follows. */
function firstLine(text: string): string {
  const lines = text.split('\n');
  return lines.length > 1 ? `${lines[0]} …` : text;
}

const SUBAGENT_TOOLS = new Set(['Task', 'Agent', 'task', 'spawnAgent']);

/** Splits `mcp__server__tool`, the name Claude Code and the legacy events give an MCP call. */
function mcpName(call: AcpToolCall): { server: string; tool: string } | null {
  const name = call.meta?.toolName ?? '';
  const m = /^mcp__([^_]+(?:_[^_]+)*?)__(.+)$/.exec(name);
  if (m?.[1] && m[2]) return { server: m[1], tool: m[2] };
  const server = call.rawInput?.server;
  const tool = call.rawInput?.tool;
  if (typeof server === 'string' && typeof tool === 'string') return { server, tool };
  return null;
}

function stepKind(call: AcpToolCall): StepKind {
  const mcp = mcpName(call);
  if (mcp) return mcp.server === 'runtime' && mcp.tool === 'provision_sandbox' ? 'sandbox' : 'mcp';
  if (SUBAGENT_TOOLS.has(call.meta?.toolName ?? '')) return 'subagent';
  switch (call.kind) {
    case 'delete':
    case 'move':
      return 'edit';
    case 'switch_mode':
      return 'other';
    default:
      return call.kind;
  }
}

const VERBS: Record<StepKind, [running: string, done: string]> = {
  read: ['Reading', 'Read'],
  edit: ['Editing', 'Edited'],
  execute: ['Running', 'Ran'],
  search: ['Searching', 'Searched'],
  fetch: ['Fetching', 'Fetched'],
  think: ['Thinking', 'Thought'],
  mcp: ['Calling', 'Called'],
  subagent: ['Subagent', 'Subagent'],
  sandbox: ['Starting sandbox', 'Sandbox ready'],
  // An unnamed call's title already says what it did.
  other: ['', ''],
};

/** A non-empty string value, or undefined. */
export const str = (v: unknown): string | undefined => (typeof v === 'string' && v ? v : undefined);

/** The title without the harness's leading verb ("Run ", "Read ", "Search "). */
function titleObject(title: string): string {
  return title.replace(/^(Run|Read|Search|Searched|Edit|Write|Fetch|Grep|Glob|List)\s+/i, '');
}

function targetOf(kind: StepKind, call: AcpToolCall): string {
  const input = call.rawInput ?? {};
  switch (kind) {
    case 'read':
    case 'edit': {
      const path =
        str(input.file_path) ??
        str(input.filePath) ??
        str(input.path) ??
        call.diffs?.[0]?.path ??
        call.changes?.[0]?.path;
      return shortPath(path ?? titleObject(call.title));
    }
    case 'execute':
      // Codex records some commands with an empty `command`; its title still names them.
      return firstLine(str(input.command) ?? titleObject(call.title));
    case 'search':
      return str(input.pattern) ?? str(input.query) ?? titleObject(call.title);
    case 'fetch':
      return str(input.url) ?? titleObject(call.title);
    case 'subagent':
      return str(input.description) ?? str(input.prompt)?.split('\n')[0] ?? call.title;
    case 'sandbox':
      return [str(input.image), str(input.size)].filter(Boolean).join(' · ');
    case 'mcp': {
      const mcp = mcpName(call);
      return mcp ? `${mcp.server} · ${mcp.tool}` : call.title;
    }
    default:
      return call.title;
  }
}

/** Past this many cells the line diff is skipped and only the line counts are shown. */
const DIFF_CELLS = 250_000;

/** Unified lines of an ACP diff (whole-file before and after), with two lines of context. */
function fileDiff(d: { path: string; oldText: string | null; newText: string }): FileDiff {
  const a = d.oldText === null ? [] : d.oldText.split('\n');
  const b = d.newText.split('\n');
  if (a.length * b.length > DIFF_CELLS)
    return { path: shortPath(d.path), added: b.length, removed: a.length, lines: null };
  // Longest common subsequence table; edited files here are small.
  const lcs = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i--)
    for (let j = b.length - 1; j >= 0; j--)
      lcs[i]![j] =
        a[i] === b[j] ? lcs[i + 1]![j + 1]! + 1 : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!);
  const all: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      all.push({ kind: 'ctx', text: a[i]! });
      i++;
      j++;
    } else if (j < b.length && (i >= a.length || lcs[i]![j + 1]! >= lcs[i + 1]![j]!)) {
      all.push({ kind: 'add', text: b[j]! });
      j++;
    } else {
      all.push({ kind: 'del', text: a[i]! });
      i++;
    }
  }
  const near = (k: number) => all.slice(Math.max(0, k - 2), k + 3).some((l) => l.kind !== 'ctx');
  return {
    path: shortPath(d.path),
    added: all.filter((l) => l.kind === 'add').length,
    removed: all.filter((l) => l.kind === 'del').length,
    lines: all.filter((l, k) => l.kind !== 'ctx' || near(k)),
  };
}

function errorLineOf(output: string): string {
  return (
    output
      .split('\n')
      .map((l) => l.trim())
      .find((l) => l && !/^Exit code \d+$/.test(l)) ?? ''
  );
}

export function stepOf(call: AcpToolCall): Step {
  const kind = stepKind(call);
  const running = call.status === 'in_progress';
  const status: Step['status'] =
    call.policy && !call.policy.allowed
      ? 'denied'
      : running
        ? 'running'
        : call.status === 'failed'
          ? 'failed'
          : 'done';
  const [runningVerb, doneVerb] = VERBS[kind];
  const mcp = kind === 'mcp' ? mcpName(call) : null;
  const diffs =
    call.diffs?.map(fileDiff) ??
    call.changes?.map((c) => ({
      path: shortPath(c.path),
      added: 0,
      removed: 0,
      lines: null,
    }));
  return {
    id: call.id,
    kind,
    verb: running ? runningVerb : doneVerb,
    target: targetOf(kind, call),
    status,
    durationMs:
      call.endedAt === undefined || call.startedAt === undefined
        ? null
        : Math.max(0, call.endedAt - call.startedAt),
    ...(kind === 'execute'
      ? { command: str(call.rawInput?.command) ?? titleObject(call.title) }
      : {}),
    ...(call.output !== undefined ? { output: call.output } : {}),
    ...(call.exitCode !== undefined ? { exitCode: call.exitCode } : {}),
    ...(status === 'failed' && call.output ? { errorLine: errorLineOf(call.output) } : {}),
    ...(diffs ? { diffs } : {}),
    ...(mcp
      ? {
          mcp: {
            ...mcp,
            args: Object.fromEntries(
              Object.entries(call.rawInput ?? {}).filter(([k]) => k !== 'server' && k !== 'tool'),
            ),
          },
        }
      : {}),
    ...(call.policy ? { policy: call.policy } : {}),
    ...(call.truncated ? { truncated: call.truncated } : {}),
    call,
  };
}

// ── Timeline rows ────────────────────────────────────────────────────────────

/** Reads and searches that finished cleanly: the calls a reader skims past. */
const QUIET: ReadonlySet<StepKind> = new Set(['read', 'search', 'think']);

export type Row =
  | { type: 'entry'; entry: Exclude<Entry, { type: 'call' }> }
  | { type: 'step'; step: Step; children: Step[] }
  | { type: 'quiet'; id: string; steps: Step[] };

function isQuiet(step: Step): boolean {
  return QUIET.has(step.kind) && step.status === 'done';
}

/** Which fold a step can join: clean reads and searches, or clean edits. */
function foldClass(step: Step): 'quiet' | 'edit' | null {
  if (isQuiet(step)) return 'quiet';
  if (step.kind === 'edit' && step.status === 'done') return 'edit';
  return null;
}

/** Fewest steps in a row that fold: two reads, or three edits (fewer edits read better whole). */
const FOLD_MIN = { quiet: 2, edit: 3 } as const;

/**
 * The timeline in time order. A subagent's calls nest under it, a read of the file the next step
 * edits is part of that edit, and a run of clean reads or clean edits folds into one line.
 * Nothing is hoisted: a failure stays where it happened.
 */
export function rowsOf(entries: Entry[]): Row[] {
  const children = new Map<string, Step[]>();
  const items: (Step | Exclude<Entry, { type: 'call' }>)[] = [];
  for (const e of entries) {
    if (e.type !== 'call') {
      items.push(e);
      continue;
    }
    const step = stepOf(e.call);
    const parent = e.call.meta?.parentId;
    if (parent) children.set(parent, [...(children.get(parent) ?? []), step]);
    else items.push(step);
  }
  const absorbed = new Set<string>();
  items.forEach((item, i) => {
    const next = items[i + 1];
    if ('kind' in item && next && 'kind' in next && item.kind === 'read' && item.status === 'done')
      if (next.kind === 'edit' && next.target === item.target) absorbed.add(item.id);
  });
  const rows: Row[] = [];
  let fold: Step[] = [];
  let foldKind: 'quiet' | 'edit' | null = null;
  const flush = () => {
    const head = fold[0];
    if (head && foldKind && fold.length >= FOLD_MIN[foldKind])
      rows.push({ type: 'quiet', id: `q-${head.id}`, steps: fold });
    else for (const step of fold) rows.push({ type: 'step', step, children: [] });
    fold = [];
    foldKind = null;
  };
  for (const item of items) {
    if (!('kind' in item)) {
      flush();
      rows.push({ type: 'entry', entry: item });
      continue;
    }
    if (absorbed.has(item.id)) continue;
    const cls = foldClass(item);
    if (cls && (cls === foldKind || fold.length === 0)) {
      fold.push(item);
      foldKind = cls;
      continue;
    }
    flush();
    if (cls) {
      fold.push(item);
      foldKind = cls;
      continue;
    }
    rows.push({ type: 'step', step: item, children: children.get(item.id) ?? [] });
  }
  flush();
  return rows;
}

/** "Read 3 files, searched 2 times": what a folded line stands for. */
export function quietSummary(steps: Step[]): string {
  const count = (k: StepKind) => steps.filter((s) => s.kind === k).length;
  const parts: string[] = [];
  const reads = count('read');
  const searches = count('search');
  const thoughts = count('think');
  const edits = count('edit');
  if (edits) parts.push(`edited ${edits} ${edits === 1 ? 'file' : 'files'}`);
  if (reads) parts.push(`read ${reads} ${reads === 1 ? 'file' : 'files'}`);
  if (searches) parts.push(`searched ${searches} ${searches === 1 ? 'time' : 'times'}`);
  if (thoughts) parts.push(`thought ${thoughts === 1 ? 'once' : `${thoughts} times`}`);
  const text = parts.join(', ');
  return text.charAt(0).toUpperCase() + text.slice(1);
}

// ── People ───────────────────────────────────────────────────────────────────

export interface Participant {
  name: string;
  kind: 'person' | 'agent' | 'trigger';
  /** How they took part: "Started it via Slack", "2 messages", "1 message · by hand". */
  detail: string;
}

const PROVIDER_NAME: Record<string, string> = {
  github: 'GitHub',
  slack: 'Slack',
  alertmanager: 'Alertmanager',
};

/**
 * Everyone involved, from the run's messages: people by their recorded sender, people outside
 * the platform by the event a trigger carried (a GitHub login), and triggers with no person behind them.
 */
export function participantsOf(run: RunView): Participant[] {
  const by = new Map<
    string,
    { kind: Participant['kind']; messages: number; started: boolean; via?: string }
  >();
  let first = true;
  for (const e of run.entries) {
    if (e.type !== 'message') continue;
    // A sender the run never recorded is nobody the rail can name.
    if (e.author.unrecorded) {
      first = false;
      continue;
    }
    const actor = e.event?.actor;
    const person = e.author.kind === 'person' || actor?.kind === 'person';
    const kind: Participant['kind'] = person
      ? 'person'
      : e.author.kind === 'agent'
        ? 'agent'
        : 'trigger';
    const name =
      e.author.kind === 'trigger' && actor?.kind === 'person' ? actor.name : e.author.name;
    const prev = by.get(name);
    // How they reached the run: by hand, or through a provider's trigger.
    const via =
      e.author.kind !== 'trigger'
        ? undefined
        : e.author.via === 'manual'
          ? 'by hand'
          : `via ${(e.author.provider && PROVIDER_NAME[e.author.provider]) || e.author.name}`;
    by.set(name, {
      kind,
      messages: (prev?.messages ?? 0) + 1,
      started: prev?.started ?? first,
      via: prev?.via ?? via,
    });
    first = false;
  }
  return [...by].map(([name, p]) => {
    const count = `${p.messages} ${p.messages === 1 ? 'message' : 'messages'}`;
    const parts = p.started
      ? [p.via ? `Started it ${p.via}` : 'Started it', p.messages > 1 ? count : '']
      : [count, p.via ?? ''];
    return { name, kind: p.kind, detail: parts.filter(Boolean).join(' · ') };
  });
}

/** A command without the plumbing: a leading `cd` dropped and a chain split into its commands. */
export function commandParts(step: Step): string[] {
  if (step.kind !== 'execute') return [step.target];
  const parts = step.target
    .split(/;\s*|&&\s*/)
    .map((p) => p.trim())
    .filter((p) => p && !/^cd\s+\S+$/.test(p));
  return parts.length ? parts : [step.target];
}

/** Elapsed time at `now` (ms): to the end for an ended run, to now for a live one. */
export function elapsedMs(run: RunView, now: number): number {
  const start = run.startedAt ? new Date(run.startedAt).getTime() : NaN;
  const end = run.endedAt ? new Date(run.endedAt).getTime() : now;
  return Number.isNaN(start) ? 0 : Math.max(0, end - start);
}

/** "1.2K", or the number itself under a thousand. */
export const compact = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}K` : String(n));

/** The run's token use, as the rail and the end line both show it. */
export function tokensLine(run: RunView): string {
  const r = run.record;
  if (!r) return '';
  return `${compact(r.inputTokens)} in · ${compact(r.outputTokens)} out · ${compact(r.cachedTokens)} cached`;
}

export const ENDED: ReadonlySet<RunState> = new Set([
  'completed',
  'failed',
  'budget_exhausted',
  'terminated',
]);

/** "4m 05s", "18s", "820ms". */
export function formatSpan(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`;
}

/** A local wall-clock time as "09:14". */
export function clockOf(iso: string): string {
  const t = new Date(iso);
  return Number.isNaN(t.getTime()) ? '' : t.toTimeString().slice(0, 5);
}
