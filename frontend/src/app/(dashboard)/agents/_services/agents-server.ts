import 'server-only';

import type { AgentType, Run } from '@orbit/shared/api';
import { MOCK_AGENT_TYPES, MOCK_RUNS } from '../../../api/agents/_mock/data';
import { toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

export type AgentsResult =
  | { ok: true; agentTypes: AgentTypeRow[]; runs: RunRow[] }
  | { ok: false; status: number; message: string };

export async function fetchAgentsFromBFF(accessToken: string): Promise<AgentsResult> {
  void accessToken;
  const agentTypes = (MOCK_AGENT_TYPES as AgentType[]).map(toAgentTypeRow);
  const runs = (MOCK_RUNS as Run[]).map((r) => toRunRow(r, agentTypes));
  return { ok: true, agentTypes, runs };
}
