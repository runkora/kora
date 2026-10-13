import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

import type { AgentBuilderStateShape } from '../../_hooks/use-agent-builder';
import { StepReview } from './step-review';

const builder = {
  fields: {
    name: 'Coder',
    slug: 'coder',
    identityMode: 'autonomous',
    framework: 'claude-code',
    modelId: '',
    systemPrompt: '',
    mcpTools: [],
    connectors: [],
    connectivityMode: 'none',
    environments: [
      { environment_id: 'env-go', is_default: true },
      { environment_id: 'env-gone', is_default: false },
    ],
    agentBudgetOn: false,
    runBudgetOn: false,
    capacityOn: false,
    repeatedCallOn: false,
    waitRearmOn: false,
  },
  catalogues: {
    models: [],
    environments: [{ id: 'env-go', name: 'Go services' }],
    status: { environments: 'success' },
  },
  goTo: () => {},
  createBlockers: [],
  submitError: null,
} as unknown as AgentBuilderStateShape;

vi.mock('../../_context/agent-builder-context', () => ({
  useAgentBuilderContext: () => builder,
}));

describe('StepReview', () => {
  it('names an attached environment that no longer exists instead of showing its id', () => {
    render(<StepReview />);
    expect(
      screen.getByText('Go services, Deleted environment (default: Go services)'),
    ).toBeInTheDocument();
    expect(screen.queryByText(/env-gone/)).toBeNull();
  });

  it('does not report attached environments as deleted while the catalogue loads', () => {
    builder.catalogues.status.environments = 'loading';
    render(<StepReview />);
    expect(screen.getByText('Loading environments.')).toBeInTheDocument();
    expect(screen.queryByText(/Deleted environment/)).toBeNull();
    builder.catalogues.status.environments = 'success';
  });
});
