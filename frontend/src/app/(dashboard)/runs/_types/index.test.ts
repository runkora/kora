import { describe, expect, it } from 'vitest';
import { DEFAULT_RUN_FILTERS, runFilterParams, type RunFilters } from '.';

const query = (patch: Partial<RunFilters>) =>
  runFilterParams({ ...DEFAULT_RUN_FILTERS, ...patch }).toString();

describe('runFilterParams', () => {
  it.each([
    ['all', ''],
    ['waiting', 'state=idle'],
    ['mine', 'created_by=me'],
    ['active', 'state=pending%2Cstarting%2Cactive%2Ccompleting'],
    ['failed', 'state=failed%2Cbudget_exhausted'],
  ] as const)('sends the %s quick filter as %j', (quick, expected) => {
    expect(query({ quick })).toBe(expected);
  });

  it('sends the agent, the source and the trimmed search', () => {
    expect(query({ agentTypeId: 'agt_1', source: 'none', query: '  retry  ' })).toBe(
      'agent_type_id=agt_1&trigger_kind=none&q=retry',
    );
  });

  it('sends no search for blank text', () => {
    expect(query({ query: '   ' })).toBe('');
  });
});
