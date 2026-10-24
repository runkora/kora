import { describe, expect, it } from 'vitest';
import type { RunWithAttribution } from '@runkora/shared/api';
import { indexAgentTypes, toRunRow } from './index';

const run: RunWithAttribution = {
  id: 'run-1',
  tenant_id: 't-1',
  agent_type_id: 'at-1',
  state: 'completed',
  pod_ordinal: 0,
  created_by: 'alice',
  started_at: null,
  completed_at: null,
  created_at: '2026-10-24T00:00:00Z',
  updated_at: '2026-10-24T00:00:00Z',
  identity_mode: 'delegated',
  agent_slug: 'reviewer',
  agent_name: 'Reviewer',
};

describe('toRunRow', () => {
  it('names the agent from the run when the agent list no longer has it', () => {
    const row = toRunRow(run, indexAgentTypes([]));
    expect(row.agentTypeName).toBe('Reviewer');
    expect(row.agentTypeSlug).toBe('reviewer');
    expect(row.delegated).toBe(true);
  });
});
