import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { DeliveryDetail } from './delivery-detail';
import { DeliveryRouting, fallbackText, thresholdLabel } from './delivery-routing';
import { toDeliveryRow, toDeliveryRoutingRow } from '../_types';
import type { DeliveryRoutingRow } from '../_types';
import type { TriggerDelivery, TriggerRoutingDecision } from '@runkora/shared/api';

const AGENTS = [
  { id: 'at_rev', slug: 'code-reviewer', name: 'Code Reviewer' },
  { id: 'at_sec', slug: 'security-reviewer', name: 'Security Reviewer' },
  { id: 'at_cod', slug: 'coder', name: 'Coder' },
];

function decision(overrides: Partial<TriggerRoutingDecision> = {}): DeliveryRoutingRow {
  return toDeliveryRoutingRow({
    outcome: 'new_run',
    key_matched: false,
    join_key: 'acme/platform#1427',
    model: 'liquid/d1',
    agent_type_id: 'at_rev',
    probability: 0.91,
    threshold: 0.7,
    scores: { agents: { at_cod: 0.03, at_rev: 0.91, at_sec: 0.06 } },
    fallback_reason: null,
    error: null,
    ...overrides,
  });
}

describe('toDeliveryRoutingRow', () => {
  it('orders the agent scores highest first', () => {
    expect(decision().agentScores.map((s) => s.agentTypeId)).toEqual([
      'at_rev',
      'at_sec',
      'at_cod',
    ]);
  });
});

describe('fallbackText', () => {
  it('names the threshold the best score missed', () => {
    expect(thresholdLabel(0.7)).toBe('Balanced (0.70)');
    expect(fallbackText(decision({ outcome: 'fallback_run', probability: 0.48 }))).toBe(
      'Used. Best score 0.48 is below Balanced (0.70).',
    );
    expect(fallbackText(decision({ outcome: 'no_run', probability: 0.48, threshold: 0.85 }))).toBe(
      'Not set. Best score 0.48 is below Strict (0.85).',
    );
    expect(fallbackText(decision())).toBe('Not used');
  });

  it('says when the model failed or the chosen run closed', () => {
    expect(fallbackText(decision({ outcome: 'fallback_run', error: 'timeout' }))).toBe(
      'Used. The decision model did not answer.',
    );
    expect(fallbackText(decision({ outcome: 'no_run', error: 'timeout' }))).toBe(
      'Not set. The decision model did not answer.',
    );
    expect(fallbackText(decision({ fallback_reason: 'run_closed' }))).toMatch(/run closed first/);
  });
});

describe('DeliveryRouting', () => {
  it('shows the chosen agent, its confidence, the new run and every score', () => {
    render(<DeliveryRouting routing={decision()} runId="run_7c21" agents={AGENTS} />);
    expect(screen.getByText('Routing')).toBeTruthy();
    expect(screen.getAllByText('Code Reviewer').length).toBeGreaterThan(0);
    expect(screen.getByText('confidence')).toBeTruthy();
    expect(screen.getByText('New run')).toBeTruthy();
    expect(screen.getByText('run_7c21')).toBeTruthy();
    expect(screen.getByText('No open run for this key')).toBeTruthy();
    expect(screen.getByText('liquid/d1')).toBeTruthy();
    expect(screen.getByText('Routes at Balanced (0.70) or above')).toBeTruthy();
    expect(screen.getByText('Security Reviewer')).toBeTruthy();
  });

  it('shows a key match without a model call', () => {
    render(
      <DeliveryRouting
        routing={decision({
          outcome: 'joined_run',
          key_matched: true,
          model: null,
          probability: null,
          threshold: null,
          scores: {},
        })}
        runId="run_3f9c"
        agents={AGENTS}
      />,
    );
    expect(screen.getByText('joined by key')).toBeTruthy();
    expect(screen.getByText('Joined run')).toBeTruthy();
    expect(screen.getByText('Matched an open run')).toBeTruthy();
    expect(screen.getByText('Not called. The key decided the run.')).toBeTruthy();
    expect(screen.queryByText('confidence')).toBeNull();
    expect(screen.queryByText('Scores')).toBeNull();
  });

  it('shows the fallback and the model error', () => {
    render(
      <DeliveryRouting
        routing={decision({
          outcome: 'fallback_run',
          agent_type_id: 'at_cod',
          scores: {},
          probability: null,
          fallback_reason: 'decision_failed',
          error: 'decision model: context deadline exceeded',
        })}
        runId="run_51be"
        agents={AGENTS}
      />,
    );
    expect(screen.getByText('fallback')).toBeTruthy();
    expect(
      screen.getByText(
        'The decision model call failed, so the delivery went to the fallback agent.',
      ),
    ).toBeTruthy();
    expect(screen.getByText('decision model: context deadline exceeded')).toBeTruthy();
  });

  it('shows no run when kora was unsure and no fallback is set', () => {
    render(
      <DeliveryRouting
        routing={decision({ outcome: 'no_run', agent_type_id: null, probability: 0.48 })}
        runId={null}
        agents={AGENTS}
      />,
    );
    expect(screen.getAllByText('No run started').length).toBe(2);
    expect(screen.getByText('kora was not sure enough to route')).toBeTruthy();
    expect(screen.getByText('Not set. Best score 0.48 is below Balanced (0.70).')).toBeTruthy();
  });

  it('falls back to the id for an agent it cannot name', () => {
    render(<DeliveryRouting routing={decision()} runId="r" agents={[]} />);
    expect(screen.getAllByText('at_rev').length).toBeGreaterThan(0);
  });
});

describe('DeliveryDetail with a routed delivery', () => {
  const wire = (
    status: string,
    routing: TriggerRoutingDecision,
    error?: string,
  ): TriggerDelivery => ({
    id: 'd-1',
    tenant_id: 'x',
    kind: 'webhook',
    status: status === 'unrouted' ? 'skipped' : 'dispatched',
    received_at: '2026-10-19T09:00:00Z',
    runs: [
      {
        id: 'dr-1',
        delivery_id: 'd-1',
        trigger_id: 't1',
        trigger_name: 'route-github-work',
        status,
        ...(status === 'unrouted' ? {} : { run_id: 'run_7c21' }),
        ...(error !== undefined && { error }),
        routing,
        created_at: '2026-10-19T09:00:01Z',
      },
    ],
  });
  const routing: TriggerRoutingDecision = {
    outcome: 'no_run',
    key_matched: false,
    join_key: null,
    model: 'liquid/d1',
    agent_type_id: null,
    probability: 0.4,
    threshold: 0.7,
    scores: { agents: { at_rev: 0.4, at_cod: 0.35 } },
    fallback_reason: 'below_threshold',
    error: null,
  };

  it('leads with the routing section on the delivery panel', () => {
    render(
      <DeliveryDetail
        delivery={toDeliveryRow(
          wire('dispatched', { ...routing, outcome: 'new_run', agent_type_id: 'at_rev' }),
        )}
        agents={AGENTS}
      />,
    );
    expect(screen.getByText('Routing')).toBeTruthy();
  });

  it('reads an unrouted outcome as information, not a failure', () => {
    const reason = 'kora was not sure which agent fits, and the trigger has no fallback agent';
    render(
      <DeliveryDetail
        delivery={toDeliveryRow(wire('unrouted', routing, reason))}
        triggerId="t1"
        agents={AGENTS}
      />,
    );
    expect(screen.getByText('Routing')).toBeTruthy();
    expect(screen.getByText(reason)).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows a hold reason as the delivery reason', () => {
    const held = toDeliveryRow({
      ...wire('dispatched', routing),
      status: 'verified',
      runs: [],
      reason: 'waiting for run run-1 to start before joining it (hold 1 of 6)',
    });
    render(<DeliveryDetail delivery={held} agents={AGENTS} />);
    expect(
      screen.getByText('waiting for run run-1 to start before joining it (hold 1 of 6)'),
    ).toBeTruthy();
  });
});
