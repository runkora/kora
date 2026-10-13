import { describe, expect, it } from 'vitest';
import type { Environment } from '@orbit/shared/api';
import { toEnvironmentRow } from '.';

const env: Environment = {
  id: 'env-1',
  tenant_id: 't',
  slug: 'go',
  name: 'Go services',
  description: '',
  sandbox_image: 'kora-base',
  size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  egress_hosts: [],
  connectors: [],
  created_at: '',
  updated_at: '',
};

describe('toEnvironmentRow', () => {
  it('keeps the slug and the name', () => {
    expect(toEnvironmentRow(env)).toMatchObject({ slug: 'go', name: 'Go services' });
  });

  it('takes the name as the slug from an orbit-agents that sends none', () => {
    const previous = { ...env, slug: undefined, name: 'go' } as unknown as Environment;
    expect(toEnvironmentRow(previous).slug).toBe('go');
  });
});
