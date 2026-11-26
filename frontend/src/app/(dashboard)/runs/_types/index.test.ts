import { describe, expect, it } from 'vitest';
import {
  DEFAULT_RUN_FILTERS,
  runFilterParams,
  runFiltersFromUrl,
  runFiltersToUrl,
  type RunFilters,
} from '.';

describe('Runs page URL', () => {
  it('round-trips every filter', () => {
    const filters: RunFilters = {
      quick: 'mine',
      agentTypeId: 'agt_1',
      source: 'scheduled',
      query: 'retry',
    };
    const url = runFiltersToUrl(filters);
    expect(url.toString()).toBe('quick=mine&agent_type_id=agt_1&trigger_kind=scheduled&q=retry');
    expect(runFiltersFromUrl(url)).toEqual(filters);
  });

  it('writes nothing for the defaults and reads unknown values as the defaults', () => {
    expect(runFiltersToUrl(DEFAULT_RUN_FILTERS).toString()).toBe('');
    expect(runFiltersFromUrl(new URLSearchParams('quick=nope&trigger_kind=nope'))).toEqual(
      DEFAULT_RUN_FILTERS,
    );
  });
});

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
