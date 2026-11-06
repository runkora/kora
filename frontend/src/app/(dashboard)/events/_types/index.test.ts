import { describe, expect, it } from 'vitest';
import {
  NO_FILTERS,
  deliveryIdOf,
  eventSummary,
  filterParams,
  principalName,
  resourceOf,
  traceIdOf,
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

describe('traceIdOf', () => {
  it('reads a W3C trace id and refuses anything else', () => {
    expect(traceIdOf('00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01')).toBe(
      '4bf92f3577b34da6a3ce929d0e0e4736',
    );
    expect(traceIdOf('00-abc/../x?y-00f067aa0ba902b7-01')).toBeNull();
  });
});

describe('event display', () => {
  it('summarises a failed run with its error', () => {
    expect(eventSummary(event())).toBe('Run failed: sandbox_failed');
  });

  it('names the side of a tenant switch without the other tenant', () => {
    const e = (direction: string) =>
      event({
        type: 'identity.tenant.switched.v1',
        data: { entity: { session_id: 's', principal_id: 'p', direction }, occurred_at: '' },
      });
    expect(eventSummary(e('in'))).toBe('Switched into this tenant');
    expect(eventSummary(e('out'))).toBe('Switched to another tenant');
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
