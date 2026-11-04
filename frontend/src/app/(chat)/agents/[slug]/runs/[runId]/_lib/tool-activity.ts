/**
 * Human descriptions of an agent's tool calls: labels, outcome chips, group
 * summaries, and policy denials. Pure functions over ToolCall so the run chat
 * components stay presentational.
 */

import type { ToolCall } from '../_types';

export type ToolKind =
  'command' | 'read' | 'write' | 'search' | 'web' | 'agent' | 'mcp' | 'platform' | 'other';

export interface ToolDescription {
  kind: ToolKind;
  /** What the call did, in words: "Ran", "Read", "GitHub · get pull request". */
  label: string;
  /** The key argument, shown in mono: a command, path, pattern, or ids. */
  target: string;
  /** The MCP server's display name, for grouping calls to one service. */
  service?: string;
}

const RUNTIME_PREFIX = 'mcp__runtime__';
// OpenCode names MCP tools <server>_<tool>.
const OPENCODE_RUNTIME_PREFIX = 'runtime_';
const SERVICE_NAMES: Record<string, string> = { github: 'GitHub', gitlab: 'GitLab' };

function str(v: unknown): string {
  return typeof v === 'string' ? v : typeof v === 'number' ? String(v) : '';
}

function oneLine(s: string, max = 120): string {
  const line = s.split('\n')[0].trim();
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
}

function words(snake: string): string {
  return snake.replace(/[_-]+/g, ' ').trim();
}

function serviceName(server: string): string {
  return SERVICE_NAMES[server] ?? server.charAt(0).toUpperCase() + server.slice(1);
}

/** A value in a few words: never "[object Object]". */
function compact(key: string, v: unknown): string {
  if (typeof v === 'number') return /number|id$/.test(key) ? `#${v}` : String(v);
  if (typeof v === 'string') return v.length <= 60 ? v : `${v.slice(0, 59)}…`;
  if (typeof v === 'boolean') return `${key}: ${v}`;
  if (Array.isArray(v)) return `${key}: ${v.length} ${v.length === 1 ? 'item' : 'items'}`;
  if (v && typeof v === 'object') return `${key}: {${Object.keys(v).slice(0, 3).join(', ')}}`;
  return '';
}

/** Up to two arguments in brief, for calls with no obvious key argument. */
function keyArgs(input: Record<string, unknown>): string {
  return Object.entries(input)
    .map(([k, v]) => compact(k, v))
    .filter(Boolean)
    .slice(0, 2)
    .join(' ');
}

/** The paths a Codex fileChange touched. */
function changedPaths(changes: unknown): string {
  if (!Array.isArray(changes)) return '';
  const paths = changes.map((c: { path?: unknown }) => str(c?.path)).filter(Boolean);
  return paths.length > 2
    ? `${paths.slice(0, 2).join(', ')} +${paths.length - 2}`
    : paths.join(', ');
}

/** A Codex subagent call's receivers, counted. */
function subagents(input: Record<string, unknown>): string {
  const ids = Array.isArray(input.receiver_thread_ids) ? input.receiver_thread_ids.length : 0;
  return ids ? plural(ids, 'subagent', 'subagents') : '';
}

/**
 * Describes a call by its name and its own arguments, the shape every runtime
 * records. Claude Code, Codex (commandExecution, fileChange, spawnAgent, ...)
 * and OpenCode (bash, read, task, ...) name the same actions differently.
 */
export function describeTool(name: string, input: Record<string, unknown>): ToolDescription {
  switch (name) {
    case 'Bash':
    case 'bash':
    case 'commandExecution':
      return { kind: 'command', label: 'Ran', target: oneLine(str(input.command)) };
    case 'Monitor':
      return {
        kind: 'command',
        label: 'Monitored',
        target: oneLine(str(input.description) || str(input.command)),
      };
    case 'Read':
    case 'read':
      return { kind: 'read', label: 'Read', target: str(input.file_path) || str(input.filePath) };
    case 'Write':
    case 'write':
      return { kind: 'write', label: 'Wrote', target: str(input.file_path) || str(input.filePath) };
    case 'Edit':
    case 'edit':
    case 'MultiEdit':
    case 'NotebookEdit':
      return {
        kind: 'write',
        label: 'Edited',
        target: str(input.file_path) || str(input.filePath) || str(input.notebook_path),
      };
    case 'fileChange':
      return { kind: 'write', label: 'Changed files', target: changedPaths(input.changes) };
    case 'Glob':
    case 'glob':
      return { kind: 'search', label: 'Found files', target: str(input.pattern) };
    case 'Grep':
    case 'grep':
      return {
        kind: 'search',
        label: 'Searched',
        target: [str(input.pattern), str(input.path)].filter(Boolean).join(' in '),
      };
    case 'WebFetch':
      return { kind: 'web', label: 'Fetched', target: str(input.url) };
    case 'webfetch':
      return { kind: 'web', label: 'Fetched', target: str(input.url) };
    case 'WebSearch':
    case 'webSearch':
      return { kind: 'web', label: 'Searched the web', target: str(input.query) };
    case 'Agent':
    case 'Task':
    case 'task':
    case 'spawnAgent':
      return {
        kind: 'agent',
        label: 'Subagent',
        target: oneLine(str(input.description) || str(input.prompt), 80),
      };
    case 'wait':
      return { kind: 'other', label: 'Waited for', target: subagents(input) || 'subagent' };
    case 'sendInput':
      return { kind: 'other', label: 'Messaged', target: subagents(input) || 'subagent' };
    case 'resumeAgent':
      return { kind: 'other', label: 'Resumed', target: subagents(input) || 'subagent' };
    case 'closeAgent':
      return { kind: 'other', label: 'Closed', target: subagents(input) || 'subagent' };
    case 'TodoWrite':
    case 'todowrite':
      return { kind: 'other', label: 'Updated the plan', target: '' };
    case 'Skill':
      return { kind: 'other', label: 'Used skill', target: str(input.skill) || str(input.name) };
    case 'ToolSearch':
      return { kind: 'other', label: 'Loaded tools', target: str(input.query) };
  }
  if (name.startsWith(RUNTIME_PREFIX) || name.startsWith(OPENCODE_RUNTIME_PREFIX)) {
    return {
      kind: 'platform',
      label: words(name.replace(/^mcp__runtime__|^runtime_/, '')),
      target: '',
    };
  }
  const mcp = /^mcp__(.+?)__(.+)$/.exec(name);
  if (mcp) {
    const service = serviceName(mcp[1]);
    return { kind: 'mcp', label: `${service} · ${words(mcp[2])}`, target: keyArgs(input), service };
  }
  return { kind: 'other', label: name, target: keyArgs(input) };
}

const DENIAL =
  /egress denied|egress policy denied|tool not permitted|denied by policy|blocked by policy/i;

/**
 * Reports whether a call was blocked by kora policy, and names what was
 * blocked when it can: the host a command reached for, or the MCP tool.
 */
export function policyDenial(tool: ToolCall): { blocked: string } | null {
  if (!DENIAL.test(tool.result)) return null;
  const d = describeTool(tool.name, tool.input);
  if (d.kind === 'mcp') return { blocked: d.label };
  const host = /https?:\/\/([^/\s'"]+)/.exec(`${str(tool.input.command)} ${str(tool.input.url)}`);
  return { blocked: host ? host[1] : '' };
}

/** A call that must stay visible: it failed, was refused, or was denied. */
export function isFailure(tool: ToolCall): boolean {
  return tool.status === 'error' || policyDenial(tool) !== null;
}

const EMPTY_RESULTS =
  /^\((?:Bash completed with no output|No content)\)$|^No (?:matches|files) found$/;

function countLines(s: string): number {
  const t = s.replace(/\n+$/, '');
  return t ? t.split('\n').length : 0;
}

/** A short outcome chip for a finished call, from its result. */
export function outcome(tool: ToolCall): string {
  if (tool.status === 'running') return '';
  const result = tool.result.trim();
  // The harness refuses a tool outside the agent's scopes before running it.
  if (tool.status === 'error' && result.includes('No such tool available')) return 'not available';
  if (tool.status === 'error') {
    const exit = /Exit code (\d+)/.exec(result);
    return exit ? `exit ${exit[1]}` : 'failed';
  }
  if (!result || EMPTY_RESULTS.test(result)) return 'empty';
  const lines = countLines(result);
  switch (describeTool(tool.name, tool.input).kind) {
    case 'command':
      return 'exit 0';
    case 'read':
      return `${lines} lines`;
  }
  switch (tool.name.toLowerCase()) {
    case 'grep':
      return `${lines} ${lines === 1 ? 'match' : 'matches'}`;
    case 'glob':
      return `${lines} ${lines === 1 ? 'file' : 'files'}`;
  }
  try {
    const parsed: unknown = JSON.parse(result);
    if (Array.isArray(parsed)) return `${parsed.length} items`;
  } catch {
    // Not JSON: fall through to the generic chip.
  }
  return 'done';
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${(ms / 1000).toFixed(1)}s`;
  const secs = Math.round(ms / 1000);
  if (secs < 60) return `${secs}s`;
  return `${Math.floor(secs / 60)}m ${String(secs % 60).padStart(2, '0')}s`;
}

/** Duration of a finished call, or null while it runs or without times. */
export function callDuration(tool: ToolCall): number | null {
  if (tool.startedAt === undefined || tool.endedAt === undefined) return null;
  return Math.max(0, tool.endedAt - tool.startedAt);
}

/** Wall time a set of calls spanned, from the first start to the last end. */
export function spanDuration(tools: ToolCall[]): number | null {
  const starts = tools.flatMap((t) => (t.startedAt === undefined ? [] : [t.startedAt]));
  const ends = tools.flatMap((t) => (t.endedAt === undefined ? [] : [t.endedAt]));
  if (!starts.length || !ends.length) return null;
  return Math.max(0, Math.max(...ends) - Math.min(...starts));
}

function times(n: number): string {
  return n === 1 ? 'once' : n === 2 ? 'twice' : `${n} times`;
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** One line for a group of calls: "Ran 6 commands, read 3 files, called GitHub twice". */
export function summarize(tools: ToolCall[]): string {
  const counts = new Map<string, number>();
  const bump = (key: string) => counts.set(key, (counts.get(key) ?? 0) + 1);
  for (const t of tools) {
    const d = describeTool(t.name, t.input);
    bump(d.kind === 'mcp' ? `mcp:${d.service}` : d.kind);
  }
  const parts: string[] = [];
  for (const [key, n] of counts) {
    if (key.startsWith('mcp:')) parts.push(`called ${key.slice(4)} ${times(n)}`);
    else if (key === 'command') parts.push(`ran ${plural(n, 'command', 'commands')}`);
    else if (key === 'read') parts.push(`read ${plural(n, 'file', 'files')}`);
    else if (key === 'write') parts.push(`edited ${plural(n, 'file', 'files')}`);
    else if (key === 'search') parts.push(`searched ${times(n)}`);
    else if (key === 'web') parts.push(`fetched ${plural(n, 'page', 'pages')}`);
    else if (key === 'agent') parts.push(`ran ${plural(n, 'subagent', 'subagents')}`);
    else if (key === 'platform') parts.push(`made ${plural(n, 'platform call', 'platform calls')}`);
    else parts.push(`used ${plural(n, 'tool', 'tools')}`);
  }
  const line = parts.join(', ');
  return line.charAt(0).toUpperCase() + line.slice(1);
}

export function isSandboxProvision(tool: ToolCall): boolean {
  return (
    tool.name === `${RUNTIME_PREFIX}provision_sandbox` ||
    tool.name === `${OPENCODE_RUNTIME_PREFIX}provision_sandbox`
  );
}
