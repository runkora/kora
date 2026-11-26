import type { RunWithAttribution } from '@runkora/shared/api';
import type { AgentTypeRow as Agent } from '@runkora/app/(dashboard)/agents/_types';

/**
 * The agent a new run starts with. The `agent` in the address wins, even one the viewer cannot
 * run. Otherwise it is the agent of the viewer's newest run if they may still run it, and then the
 * first agent they may run.
 */
export function selectAgent(
  agents: Agent[],
  runs: RunWithAttribution[],
  slug?: string,
): Agent | null {
  const asked = slug ? agents.find((a) => a.slug === slug) : undefined;
  if (asked) return asked;
  const runnable = agents.filter((a) => a.canRun);
  const last = runnable.find((a) => a.id === runs[0]?.agent_type_id);
  return last ?? runnable[0] ?? agents[0] ?? null;
}
