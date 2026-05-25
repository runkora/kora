import 'server-only';

import type { AgentType, Run } from '@orbit/shared/api';
import { MOCK_AGENT_TYPES, MOCK_RUNS } from '../../../../api/agents/_mock/data';
import { toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';

export type AgentDetailResult =
  | { ok: true; agent: AgentTypeRow; runs: RunRow[] }
  | { ok: false; status: number; message: string };

export async function fetchAgentDetailFromBFF(
  slug: string,
  accessToken: string,
): Promise<AgentDetailResult> {
  void accessToken;
  const found = (MOCK_AGENT_TYPES as AgentType[]).find((a) => a.slug === slug);
  if (!found) {
    return { ok: false, status: 404, message: 'Agent not found' };
  }

  const agent = toAgentTypeRow(found);
  const runs = (MOCK_RUNS as Run[])
    .filter((r) => r.agent_type_id === found.id)
    .map((r) => toRunRow(r, [agent]));

  return { ok: true, agent, runs };
}
