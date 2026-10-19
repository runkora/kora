import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { EffectiveAccess } from '@runkora/shared/api';
import { EffectiveAccessPanel } from './effective-access-panel';

const base: EffectiveAccess = {
  open: false,
  own_hosts: [],
  environments: [],
  environment_hosts: [],
  connector_egress: [],
  connectors: [],
  hosts: [],
};

describe('EffectiveAccessPanel', () => {
  it('lists what each attached environment contributes', () => {
    render(
      <EffectiveAccessPanel
        access={{
          ...base,
          environments: [
            { id: 'go', name: 'Go services', hosts: ['proxy.golang.org'] },
            { id: 'node', name: 'Node apps', hosts: ['registry.npmjs.org'] },
          ],
          environment_hosts: ['proxy.golang.org', 'registry.npmjs.org'],
          hosts: ['proxy.golang.org', 'registry.npmjs.org'],
        }}
        loading={false}
        error={null}
      />,
    );
    expect(screen.getByText('2 attached')).toBeInTheDocument();
    expect(screen.getByText('Go services')).toBeInTheDocument();
    expect(screen.getByText('Node apps')).toBeInTheDocument();
  });

  it('says when no environment is attached', () => {
    render(<EffectiveAccessPanel access={base} loading={false} error={null} />);
    expect(screen.getByText('No environments attached.')).toBeInTheDocument();
  });
});
