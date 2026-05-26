// ── Tool / run status enums ───────────────────────────────────────────────────

export type ToolStatus = 'running' | 'done' | 'error';

export type RunState = 'pending' | 'starting' | 'active' | 'completing' | 'completed' | 'failed';

// ── Turn types ────────────────────────────────────────────────────────────────

export interface HumanTurn {
  type: 'human';
  id: string;
  content: string;
  timestamp: string;
}

export interface ToolCall {
  id: string;
  /** Emoji icon: '📄' | '✏️' | '⚡' | '🔍' */
  icon: string;
  name: string;
  /** Truncated display argument (path, command snippet, etc.) */
  arg: string;
  status: ToolStatus;
  /** Display result: '✓ 142 lines' | '✗ 3 errors' | '+12 / -3' — empty when running */
  result: string;
  /** When set, the pop-out button (⤢) opens this canvas view. */
  canvasViewId?: string;
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
}

export type AgentElement =
  | { kind: 'text'; content: string }
  | { kind: 'tool'; tool: ToolCall }
  | { kind: 'approval'; id: string; title: string; description: string; command: string };

export type Turn = HumanTurn | AgentTurn | ErrorTurn;

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
  runState: RunState;
  turns: Turn[];
  contextFiles: string[];
  canvasState: CanvasState;
  views: Record<string, CanvasView>;
}
