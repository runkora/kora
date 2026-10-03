import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import type { Environment } from '@orbit/shared/api';
import { loopGuardrailValues } from '../../_types';
import { AgentConfigurationTab } from './agent-configuration-tab';

vi.mock('@orbit/components', () => ({
  ConnectionCards: () => null,
  EffectiveAccessPanel: () => null,
  ModelPicker: () => null,
}));

const GO: Environment = {
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
};

// Only the fields the tab reads matter here; every other key is an inert function.
let cfg: Record<string, unknown>;
function makeCfg(fields: Record<string, unknown>): Record<string, unknown> {
  return new Proxy(fields, {
    get: (target, key: string) => (key in target ? target[key] : vi.fn()),
  });
}
const BASE = {
  agent: { identityMode: 'autonomous', createdAt: '', updatedAt: '' },
  name: 'Coder',
  description: '',
  framework: 'claude-code',
  systemPrompt: '',
  modelId: '',
  models: [],
  mcpTools: [],
  mcpServers: [],
  mcpCatalog: [],
  connectors: [],
  availableConnections: [],
  connectivityMode: 'none',
  connectivityHosts: '',
  environments: [{ environment_id: 'env-go', is_default: true }],
  environmentCatalogue: [],
  environmentsError: null,
  environmentsLoading: true,
  maxConcurrentRuns: '10',
  loopGuardrails: loopGuardrailValues(undefined),
  budgetLimitUSD: '',
  budgetCadence: 'monthly',
  budgetPerRunUSD: '',
  isDirty: false,
  deleteOpen: false,
};
vi.mock('../_hooks/use-agent-configuration', () => ({
  useAgentConfiguration: () => cfg,
}));

describe('AgentConfigurationTab environments', () => {
  beforeEach(() => {
    cfg = makeCfg({ ...BASE });
  });

  function openNetwork() {
    render(<AgentConfigurationTab />);
    fireEvent.click(screen.getByText('Network access', { selector: 'button *' }));
  }

  it('holds the environments card back while the catalogue loads', () => {
    openNetwork();
    expect(screen.queryByText('Deleted environment')).toBeNull();
    expect(screen.queryByLabelText('Go services')).toBeNull();
  });

  it('shows the attached environments once the catalogue loads', () => {
    cfg = makeCfg({ ...BASE, environmentsLoading: false, environmentCatalogue: [GO] });
    openNetwork();
    expect(screen.getByLabelText('Go services')).toBeChecked();
    expect(screen.queryByText('Deleted environment')).toBeNull();
  });
});
