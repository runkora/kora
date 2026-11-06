import { describe, expect, it } from 'vitest';
import {
  NO_FILTERS,
  deliveryIdOf,
  eventSummary,
  filterParams,
  principalName,
  resourceOf,
  triggerIdsOf,
} from './index';
import type { KoraEvent } from './index';

const event = (over: Partial<KoraEvent> = {}): KoraEvent => ({
  specversion: '1.0',
  id: 'e1',
  source: 'kora-agents',
  type: 'agents.run.failed.v1',
  subject: 'runs/2b4e8c1d-5f7a-4c2e-9b1d-0e6f3a8c7d21',
  time: '2026-11-06T03:14:07Z',
  datacontenttype: 'application/json',
  koratenant: 't',
  koraprincipal: 'agent-subject',
  koraprincipaltype: 'AGENT',
  korachaindepth: 0,
  data: { entity: { error: 'sandbox_failed' }, occurred_at: '2026-11-06T03:14:07Z' },
  ...over,
});

describe('filterParams', () => {
  it('sends every category of a tab and repeats each list filter', () => {
    const params = filterParams({
      ...NO_FILTERS,
      tab: 'triggers',
      agents: ['triage', 'pr-reviewer'],
      query: ' sandbox ',
    });
    expect(params.getAll('category')).toEqual([
      'triggers.trigger',
      'triggers.source',
      'triggers.delivery',
    ]);
    expect(params.getAll('agent')).toEqual(['triage', 'pr-reviewer']);
    expect(params.get('q')).toBe('sandbox');
  });

  it('sends nothing for no filters', () => {
    expect(filterParams(NO_FILTERS).toString()).toBe('');
  });
});

describe('event display', () => {
  it('summarises a failed run with its error', () => {
    expect(eventSummary(event())).toBe('Run failed: sandbox_failed');
  });

  it('reads a run sandbox subject as the sandbox', () => {
    const e = event({ subject: 'runs/2b4e8c1d-5f7a-4c2e-9b1d-0e6f3a8c7d21/sandbox' });
    expect(resourceOf(e)).toEqual({ kind: 'Sandbox', label: '2b4e8c1d' });
  });

  it('falls back to the subject when the event has no principal name', () => {
    expect(principalName(event())).toBe('agent-subject');
    expect(principalName(event({ koraprincipalname: 'triage' }))).toBe('triage');
  });

  it('collects the chain triggers and the delivery trigger once each', () => {
    const e = event({
      subject: 'deliveries/dlv_1',
      koratriggers: ['trg_a'],
      data: { entity: { trigger_id: 'trg_a' }, occurred_at: '' },
    });
    expect(triggerIdsOf(e)).toEqual(['trg_a']);
    expect(deliveryIdOf(e)).toBe('dlv_1');
  });
});
