// ── Tool / run status enums ───────────────────────────────────────────────────

/** `stopped` is a call still running when the user stopped the turn. */
export type ToolStatus = 'running' | 'done' | 'error' | 'stopped';

export type RunState =
  | 'pending'
  | 'starting'
  | 'active'
  | 'idle'
  | 'completing'
  | 'completed'
  | 'failed'
  | 'budget_exhausted';

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
  | { kind: 'notice'; message: string; tone?: 'error' }
  | { kind: 'approval'; id: string; title: string; description: string; command: string };

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

// ── Canvas types ──────────────────────────────────────────────────────────────

export interface CanvasTab {
  id: string;
  label: string;
  ext?: string; // 'tsx' | 'ts' | undefined — shown in accent colour
  modified?: boolean; // shows ● dot (degraded colour) when true
}

export type CanvasViewType = 'code' | 'diff' | 'terminal';

export interface DiffLine {
  kind: 'add' | 'del' | 'ctx';
  oldNum?: number;
  newNum?: number;
  content: string;
}

export interface TerminalLine {
  kind: 'normal' | 'error' | 'success' | 'dim';
  content: string;
}

export interface CanvasView {
  id: string;
  type: CanvasViewType;
  title: string;
  subtitle?: string;
  // Code view fields
  code?: string;
  language?: string;
  lineCount?: number;
  // Diff view fields
  diffLines?: DiffLine[];
  addCount?: number;
  delCount?: number;
  // Terminal view fields
  command?: string;
  exitCode?: number;
  output?: TerminalLine[];
}

// ── Top-level run state ───────────────────────────────────────────────────────

export interface CanvasState {
  isOpen: boolean;
  tabs: CanvasTab[];
  activeTabId: string | null;
}

export interface RunChatState {
  runId: string;
  slug: string;
  agentName: string;
  model: string;
  /** Null until the run's state has loaded. */
  runState: RunState | null;
  turns: Turn[];
  contextFiles: string[];
  canvasState: CanvasState;
  views: Record<string, CanvasView>;
  /** Live USD spent on this run; grows as turns complete. */
  spentUsd: number;
  /** Per-run budget ceiling in USD (from the agent's grant); null when unset. */
  perRunLimitUsd: number | null;
  /** Total tokens consumed by this run so far. */
  tokensUsed: number;
  /** LLM-generated one-sentence summary of the run's task intent, or null if not available. */
  summary: string | null;
  /** Who started the run — BFF-resolved display name, or the raw principal id, or null until loaded. */
  startedBy: string | null;
  /** True when the agent is delegated: the run acts on the starter's behalf and is private to them. */
  delegated: boolean;
  /** Whether the caller may message or stop this run. False until the run loads. */
  canSteer: boolean;
  /** Why the run could not be loaded; the page shows this instead of an empty thread. */
  loadError: string | null;
  /** Why the last Stop was refused; null when it was not. */
  stopError: string | null;
}
