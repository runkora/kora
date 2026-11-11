/**
 * Builds the run page's view from what the run hook holds: the run record, its agent and the
 * transcript the event stream replayed. Tool calls arrive with each harness's own tool name and
 * input, and are read into ACP's shape here so every harness renders the same way.
 */
import type { IconName } from '@runkora/hades';
import type { RunEventSummary, RunSource, RunWithAttribution } from '@runkora/shared/api';
import { fmtDateTime } from '@runkora/utils';
import { relativeTime } from '@runkora/lib/relative-time';
import type { HumanTurn, RunChatState, ToolCall } from '../_types';
import type { UseRunReturn } from '../_hooks/use-run';
import { describeTool, policyDenial, type ToolKind } from './tool-activity';
import {
  commandParts,
  ENDED,
  stepOf,
  type AcpKind,
  type AcpToolCall,
  type Entry,
  type Plan,
  type QueuedMessage,
  type RunView,
  type TriggerEventSummary,
  type TriggerOrigin,
} from './run-view';

const str = (v: unknown): string | undefined => (typeof v === 'string' && v ? v : undefined);

const ACP_KIND: Record<ToolKind, AcpKind> = {
  command: 'execute',
  read: 'read',
  write: 'edit',
  search: 'search',
  web: 'fetch',
  agent: 'other',
  mcp: 'other',
  platform: 'other',
  other: 'other',
};

/** The text of each file an edit call changed, from Claude Code's and OpenCode's inputs. */
function diffsOf(name: string, input: Record<string, unknown>): AcpToolCall['diffs'] {
  const path = str(input.file_path) ?? str(input.filePath) ?? str(input.notebook_path);
  if (!path) return undefined;
  if (name === 'Write' || name === 'write')
    return [{ path, oldText: null, newText: String(input.content ?? '') }];
  if (Array.isArray(input.edits))
    return input.edits.map((e: { old_string?: unknown; new_string?: unknown }) => ({
      path,
      oldText: String(e.old_string ?? ''),
      newText: String(e.new_string ?? ''),
    }));
  const oldText = input.old_string ?? input.oldString;
  const newText = input.new_string ?? input.newString;
  return typeof oldText === 'string' && typeof newText === 'string'
    ? [{ path, oldText, newText }]
    : undefined;
}

/** A Codex fileChange's paths and kinds; its text is not recorded. */
function changesOf(input: Record<string, unknown>): AcpToolCall['changes'] {
  if (!Array.isArray(input.changes)) return undefined;
  return input.changes.flatMap((c: { path?: unknown; kind?: unknown }) => {
    const path = str(c?.path);
    const kind = str(c?.kind) ?? str((c?.kind as { type?: unknown } | undefined)?.type);
    if (!path) return [];
    return [{ path, kind: kind === 'add' || kind === 'delete' ? kind : 'update' }];
  });
}

/** A call recorded under the harness's own tool name, read into ACP's shape. */
export function acpCallOf(tool: ToolCall): AcpToolCall {
  const d = describeTool(tool.name, tool.input);
  const kind: AcpKind = d.kind === 'web' && !str(tool.input.url) ? 'search' : ACP_KIND[d.kind];
  const exit = d.kind === 'command' ? /Exit code (\d+)/.exec(tool.result) : null;
  const denial = policyDenial(tool);
  // OpenCode names the platform's runtime tools runtime_<tool>.
  const toolName = tool.name.replace(/^runtime_/, 'mcp__runtime__');
  return {
    id: tool.id,
    kind,
    title: d.target ? `${d.label} ${d.target}` : d.label,
    status:
      tool.status === 'running' ? 'in_progress' : tool.status === 'error' ? 'failed' : 'completed',
    rawInput: tool.input,
    ...(tool.result ? { output: tool.result } : {}),
    ...(exit ? { exitCode: Number(exit[1]) } : {}),
    ...(d.kind === 'write' ? { diffs: diffsOf(tool.name, tool.input) } : {}),
    ...(tool.name === 'fileChange' ? { changes: changesOf(tool.input) } : {}),
    meta: { toolName, ...(tool.parentId ? { parentId: tool.parentId } : {}) },
    startedAt: tool.startedAt ?? tool.receivedAt,
    endedAt: tool.endedAt,
    ...(denial
      ? {
          policy: {
            allowed: false,
            scope: '',
            reason: denial.blocked ? `Blocked ${denial.blocked}` : 'Blocked by policy',
          },
        }
      : {}),
  };
}

const PLAN_TOOLS = new Set(['TodoWrite', 'todowrite']);
const PLAN_STATUS: Record<string, Plan['items'][number]['status']> = {
  completed: 'done',
  in_progress: 'active',
};

/** The agent's to-do list, from a TodoWrite call. */
function planOf(tool: ToolCall): Plan {
  const todos = Array.isArray(tool.input.todos) ? tool.input.todos : [];
  return {
    items: todos.map((t: { content?: unknown; status?: unknown }) => ({
      text: String(t?.content ?? ''),
      status: PLAN_STATUS[String(t?.status)] ?? 'pending',
    })),
  };
}

const PROVIDER_ICON: Record<string, IconName> = {
  github: 'github',
  slack: 'hash',
  alertmanager: 'prometheus',
};
const VIA_ICON: Record<string, IconName> = {
  manual: 'play',
  scheduled: 'clock',
  event: 'activity',
  webhook: 'webhook',
};

/** A recorded event summary, drawn with the icon of the system it came from. */
export function eventSummaryOf(e: RunEventSummary, origin: TriggerOrigin): TriggerEventSummary {
  const icon =
    (origin.provider && PROVIDER_ICON[origin.provider]) ||
    (origin.via && VIA_ICON[origin.via]) ||
    'zap';
  const p = e.preview;
  return {
    actor: { ...e.actor, ...(e.actor.kind === 'person' ? {} : { icon }) },
    verb: e.verb,
    object: e.object,
    ...(e.quote ? { quote: e.quote } : {}),
    ...(p
      ? {
          unfurl: {
            icon,
            title: p.title,
            meta: p.facts ?? [],
            ...(p.added !== undefined || p.removed !== undefined
              ? { diff: { added: p.added ?? 0, removed: p.removed ?? 0 } }
              : {}),
            ...(p.status ? { status: p.status } : {}),
          },
        }
      : {}),
  };
}

/** The provider of the delivery that started the run, from its trigger link. */
function triggerProvider(sources: RunSource[]): RunSource['provider'] {
  return sources.find((s) => s.kind === 'trigger')?.provider;
}

/**
 * A message to the run. A trigger_message is a trigger joining the run; the first message of a
 * trigger-started run is the trigger's own; any other is a person's, named by its sender.
 */
function messageEntry(
  t: HumanTurn,
  first: boolean,
  run: RunWithAttribution | null,
): Extract<Entry, { type: 'message' }> {
  const time = t.timestamp.slice(0, 5);
  const provider = triggerProvider(run?.sources ?? []);
  const started = run?.started_by;
  if (t.fromTrigger || (first && started && !t.sender)) {
    const origin: TriggerOrigin = {
      via: t.fromTrigger ? undefined : started?.trigger_kind,
      provider,
    };
    const name = t.fromTrigger ?? started?.trigger_name ?? 'Trigger';
    return {
      type: 'message',
      id: t.id,
      time,
      author: { kind: 'trigger', name, ...origin },
      text: t.content,
      ...(t.fromTrigger ? { joined: true } : {}),
      ...(t.event ? { event: eventSummaryOf(t.event, origin) } : {}),
    };
  }
  const name = t.sender?.name || (first && run?.created_by_name) || 'Unknown sender';
  return {
    type: 'message',
    id: t.id,
    time,
    author: { kind: 'person', name },
    text: t.content,
    ...(t.delivery === 'failed' ? { error: t.error ?? 'The message was not sent.' } : {}),
  };
}

/** The thread in order, and the messages still waiting for the agent's next step. */
export function entriesOf(
  turns: RunChatState['turns'],
  run: RunWithAttribution | null,
  streaming: boolean,
): { entries: Entry[]; queued: QueuedMessage[] } {
  const entries: Entry[] = [];
  const queued: QueuedMessage[] = [];
  const firstHuman = turns.find((t) => t.type === 'human');
  turns.forEach((t, ti) => {
    if (t.type === 'human') {
      if (t.delivery === 'queued') queued.push({ id: t.id, text: t.content });
      else entries.push(messageEntry(t, t === firstHuman, run));
      return;
    }
    if (t.type === 'error') {
      entries.push({
        type: 'notice',
        id: t.id,
        text: t.message,
        tone: 'error',
        ...(t.detail ? { detail: t.detail } : {}),
      });
      return;
    }
    if (t.thinking?.content) {
      entries.push({
        type: 'call',
        id: `${t.id}-think`,
        call: {
          id: `${t.id}-think`,
          kind: 'think',
          title: 'Thought',
          status: 'completed',
          output: t.thinking.content,
        },
      });
    }
    const live = streaming && ti === turns.length - 1;
    t.elements.forEach((el, i) => {
      const id = `${t.id}-${i}`;
      switch (el.kind) {
        case 'text':
          entries.push({
            type: 'text',
            id,
            text: el.content,
            ...(live && i === t.elements.length - 1 ? { streaming: true } : {}),
          });
          break;
        case 'tool':
          entries.push(
            PLAN_TOOLS.has(el.tool.name)
              ? { type: 'plan', id, plan: planOf(el.tool) }
              : { type: 'call', id: el.tool.id, call: acpCallOf(el.tool) },
          );
          break;
        case 'stopped':
          entries.push({
            type: 'notice',
            id,
            text: el.byGuardrail ? 'Paused by the loop guard' : 'Stopped',
          });
          break;
        case 'notice':
          entries.push({
            type: 'notice',
            id,
            text: el.message,
            ...(el.tone ? { tone: el.tone } : {}),
          });
          break;
      }
    });
  });
  return { entries, queued };
}

/** What the agent is doing now, in a few words. */
function activityOf(a: UseRunReturn['liveActivity']): string | undefined {
  switch (a?.kind) {
    case 'retry':
      return `Retrying the model call (${a.attempt} of ${a.maxRetries})`;
    case 'sandbox':
      return 'Starting the sandbox';
    case 'tool': {
      const s = stepOf(acpCallOf(a.tool));
      return `${s.verb} ${commandParts(s)[0]}`.trim();
    }
    case 'thinking':
      return 'Thinking';
    default:
      return undefined;
  }
}

/** The run page's view of the run at `now` (ms), which drives a live run's elapsed time. */
export function toRunView(s: UseRunReturn, now: number): RunView {
  const { run, agentType } = s;
  const state = s.runState ?? run?.state ?? 'pending';
  const startedAt = run?.started_at ?? run?.created_at ?? '';
  const start = startedAt ? new Date(startedAt).getTime() : NaN;
  const end = ENDED.has(state) && run?.completed_at ? new Date(run.completed_at).getTime() : now;
  const limit = agentType?.budget?.per_run_limit_usd;
  const b = s.budgetStop;
  const { entries, queued } = entriesOf(s.turns, run, s.isStreaming);
  const agentName = agentType?.name ?? run?.agent_name ?? 'Agent';
  return {
    id: s.runId,
    summary: run?.summary || `${agentName} run`,
    agent: { name: agentName, slug: s.slug },
    harness: agentType?.framework ?? '',
    model: agentType?.model_id ?? '',
    state,
    startedAt,
    durationMs: Number.isNaN(start) ? 0 : Math.max(0, end - start),
    sources: run?.sources ?? [],
    ...(agentType?.identity_mode === 'delegated'
      ? { actingFor: run?.created_by_name ?? run?.created_by ?? 'its starter' }
      : {}),
    spend: {
      usd: s.spentUsd,
      limitUsd: typeof limit === 'number' && limit > 0 ? limit : null,
      tokens: s.tokensUsed,
    },
    traceUrl: s.traceUrl,
    entries,
    queued,
    activity: activityOf(s.liveActivity),
    ...(run
      ? {
          record: {
            requests: run.usage?.request_count ?? 0,
            inputTokens: run.usage?.input_tokens ?? 0,
            outputTokens: run.usage?.output_tokens ?? 0,
            ...(s.sandbox
              ? { sandbox: { image: s.sandbox.image, environment: s.sandbox.environment } }
              : {}),
            connectors: (agentType?.connectors ?? []).map((c) => ({
              connection: c.connection,
              actions: c.actions,
            })),
          },
        }
      : {}),
    ...(b
      ? {
          budgetStop: {
            owner: b.owner,
            window: b.window,
            limitUsd: b.limit_usd,
            resets: b.resets_at ? fmtDateTime(b.resets_at) : '',
          },
        }
      : {}),
  };
}

/** One of the agent's earlier runs, for the sessions pane. */
export interface EarlierRun {
  id: string;
  label: string;
  /** How long ago it started: "3h". */
  meta: string;
  running: boolean;
}

export function earlierRunsOf(runs: RunWithAttribution[]): EarlierRun[] {
  return runs.map((r) => ({
    id: r.id,
    label: r.summary || `Run ${r.id.slice(0, 8)}`,
    meta: relativeTime(r.created_at).replace(/ ago$/, ''),
    running: r.state === 'active' || r.state === 'starting',
  }));
}
