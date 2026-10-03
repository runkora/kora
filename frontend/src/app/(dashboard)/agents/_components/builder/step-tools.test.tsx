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
    environments: [{ environment_id: 'env-go', is_default: true }],
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

  it('holds the environments card back while the catalogue loads', () => {
    builder.catalogues.status.environments = 'loading';
    render(<StepTools />);
    expect(screen.queryByText('Deleted environment')).toBeNull();
    expect(screen.queryByText(/attached|None attached/)).toBeNull();
  });

  it('shows the attached environments once the catalogue loads', () => {
    builder.catalogues.status.environments = 'success';
    builder.catalogues.environments = [
      {
        id: 'env-go',
        tenant_id: 't',
        name: 'Go services',
        description: '',
        sandbox_image: '',
        size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
        egress_hosts: [],
        connectors: [],
        created_at: '',
        updated_at: '',
      },
    ];
    render(<StepTools />);
    expect(screen.getByLabelText('Go services')).toBeChecked();
    expect(screen.queryByText('Deleted environment')).toBeNull();
  });
});
