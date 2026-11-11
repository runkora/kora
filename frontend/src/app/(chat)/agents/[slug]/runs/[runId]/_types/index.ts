import type {
  AgentType,
  RunBudgetStop,
  RunEventSummary,
  RunMessageSender,
  RunWithAttribution,
  SandboxEnvironmentPayload,
} from '@runkora/shared/api';

// ── Tool / run status enums ───────────────────────────────────────────────────

/** `stopped` is a call still running when the user stopped the turn. */
export type ToolStatus = 'running' | 'done' | 'error' | 'stopped';

export type { RunState } from '@runkora/shared/api';
import type { RunState } from '@runkora/shared/api';

// ── Turn types ────────────────────────────────────────────────────────────────

/**
 * Where a message sent from this page stands. `queued` means it was sent while
 * the agent was working and waits for the next tool boundary; `delivered` means
 * the agent has acted since. Absent for messages sent to an idle agent or replayed.
 */
export type Delivery = 'queued' | 'delivered' | 'failed';

export interface HumanTurn {
  type: 'human';
  id: string;
  content: string;
  timestamp: string;
  delivery?: Delivery;
  /** Why a send failed; set with delivery 'failed'. */
  error?: string;
  /**
   * Set on a message a trigger sent to this run while it was working: the
   * trigger's name, or its id when the name is unknown.
   */
  fromTrigger?: string;
  /** Who sent it, when the run recorded a sender. */
  sender?: RunMessageSender;
  /** The event behind a trigger's message, when its adapter summarized one. */
  event?: RunEventSummary;
}

export interface ToolCall {
  id: string;
  name: string;
  input: Record<string, unknown>;
  status: ToolStatus;
  /** Full result text as recorded (capped server-side at 64 KiB). */
  result: string;
  /** The recorded result was cut; originalBytes is its full size. */
  truncated?: boolean;
  originalBytes?: number;
  /** The Agent call this call was made inside, for a subagent's calls. */
  parentId?: string;
  /** Server times (ms) of the call and its result, for durations. */
  startedAt?: number;
  endedAt?: number;
  /** Browser time (ms) the call arrived, for the live elapsed counter. */
  receivedAt?: number;
  /** Latest heartbeat from a long-running tool. */
  progressSeconds?: number;
}

export interface ThinkingBlock {
  content: string;
  durationSeconds: number;
  tokenCount: number;
}

export interface AgentTurn {
  type: 'agent';
  id: string;
  timestamp: string;
  thinking?: ThinkingBlock;
  elements: AgentElement[];
}

export interface ErrorTurn {
  type: 'error';
  id: string;
  timestamp: string;
  code: string;
  message: string;
  /** The backend's own wording, shown under a friendlier message. */
  detail?: string;
}

export type AgentElement =
  | { kind: 'text'; content: string }
  | { kind: 'tool'; tool: ToolCall }
  /** A stopped turn; byGuardrail when kora's loop guard paused it, not the user. */
  | { kind: 'stopped'; byGuardrail?: boolean }
  /** A platform notice, such as a guardrail asking the agent to change approach. */
  | { kind: 'notice'; message: string; tone?: 'error' };

export type Turn = HumanTurn | AgentTurn | ErrorTurn;

/**
 * What the agent is doing while nothing visible streams, for the status line.
 * `since` is a browser time (ms).
 */
export type LiveActivity =
  | { kind: 'sandbox'; since: number }
  | { kind: 'retry'; attempt: number; maxRetries: number; error: string }
  | { kind: 'tool'; tool: ToolCall; since: number }
  | { kind: 'thinking'; since: number };

// ── Top-level run state ───────────────────────────────────────────────────────

export interface RunChatState {
  runId: string;
  slug: string;
  /** Null until the run's state has loaded. */
  runState: RunState | null;
  turns: Turn[];
  /** Live USD spent on this run; grows as turns complete. */
  spentUsd: number;
  /** Total tokens consumed by this run so far. */
  tokensUsed: number;
  /** Whether the caller may message or stop this run. Null until known, which leaves it to the API. */
  canSteer: boolean | null;
  /** Why the run could not be loaded; the page shows this instead of an empty thread. */
  loadError: string | null;
  /** Why the last Stop was refused; null when it was not. */
  stopError: string | null;
  /** The run's trace viewer link; null without a trace URL template or a sampled trace. */
  traceUrl: string | null;
  /** The run and its agent as last read; null until loaded or when unreadable. */
  run: RunWithAttribution | null;
  agentType: AgentType | null;
  /** The budget that stopped the run, when the run_state event named one. */
  budgetStop: RunBudgetStop | null;
  /** The environment and image of the run's latest sandbox. */
  sandbox: SandboxEnvironmentPayload | null;
  /** The agent's other recent runs, newest first. */
  earlierRuns: RunWithAttribution[];
}
