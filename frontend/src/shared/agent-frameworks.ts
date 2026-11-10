// Supported agent harnesses. Maintained by hand; keep in step with
// backend/internal/platform/harnesses/harnesses.go when adding a harness.
export const AGENT_FRAMEWORKS = [
  { id: 'claude-code', label: 'Claude Code', description: "Anthropic's agentic coding harness." },
  { id: 'codex', label: 'Codex', description: "OpenAI's coding agent runtime." },
  { id: 'opencode', label: 'OpenCode', description: 'Open-source, model-agnostic agent.' },
] as const;

export type AgentFramework = (typeof AGENT_FRAMEWORKS)[number]['id'];

export const FRAMEWORK_LABELS: Record<string, string> = {
  'claude-code': 'Claude Code',
  codex: 'Codex',
  opencode: 'OpenCode',
};
