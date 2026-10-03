import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

import type { AgentBuilderStateShape } from '../../_hooks/use-agent-builder';
import { StepTools } from './step-tools';

vi.mock('../../[slug]/_services/agent-detail-api', () => ({
  previewEffectiveAccess: () => new Promise(() => {}),
}));

vi.mock('@orbit/components', () => ({
  ConnectionCards: () => null,
  EffectiveAccessPanel: () => null,
}));

const builder = {
  fields: {
    mcpTools: [],
    connectors: [],
    connectivityMode: 'none',
    connectivityHosts: '',
    environments: [],
  },
  setField: () => {},
  catalogues: {
    models: [],
    connections: [],
    mcpServers: [],
    mcpCatalog: [],
    environments: [],
    status: { models: 'success', connections: 'success', mcp: 'success', environments: 'success' },
    errors: {},
  },
} as unknown as AgentBuilderStateShape;

vi.mock('../../_context/agent-builder-context', () => ({
  useAgentBuilderContext: () => builder,
}));

describe('StepTools', () => {
  it('offers governed access only, with no framework tool picker', () => {
    render(<StepTools />);

    expect(screen.getByText('Connections')).toBeTruthy();
    expect(screen.getByText('Network egress')).toBeTruthy();
    expect(screen.queryByText('Platform tools')).toBeNull();
  });
});
