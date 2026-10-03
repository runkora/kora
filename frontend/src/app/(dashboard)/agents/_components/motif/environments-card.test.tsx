import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import type { Environment } from '@orbit/shared/api';
import { EnvironmentsCard } from './environments-card';

const env = (id: string, name: string, sandboxImage = ''): Environment => ({
  id,
  tenant_id: 't',
  name,
  description: '',
  sandbox_image: sandboxImage,
  size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  egress_hosts: [],
  connectors: [],
  created_at: '',
  updated_at: '',
});

const ENVIRONMENTS = [env('go', 'Go services', 'acme-go'), env('node', 'Node apps')];

describe('EnvironmentsCard', () => {
  it('says an agent with no environments runs on the kora base image', () => {
    render(<EnvironmentsCard environments={ENVIRONMENTS} value={[]} onChange={vi.fn()} />);
    expect(screen.getByText(/runs on the kora base image/)).toBeInTheDocument();
    expect(screen.getByText('acme-go · 2 CPU · 4Gi memory · 10Gi disk')).toBeInTheDocument();
    expect(
      screen.getByText('kora base image · 2 CPU · 4Gi memory · 10Gi disk'),
    ).toBeInTheDocument();
  });

  it('attaches an environment as the default when none is attached', () => {
    const onChange = vi.fn();
    render(<EnvironmentsCard environments={ENVIRONMENTS} value={[]} onChange={onChange} />);
    fireEvent.click(screen.getByLabelText('Go services'));
    expect(onChange).toHaveBeenCalledWith([{ environment_id: 'go', is_default: true }]);
  });

  it('marks the default and moves it on request', () => {
    const onChange = vi.fn();
    render(
      <EnvironmentsCard
        environments={ENVIRONMENTS}
        value={[
          { environment_id: 'go', is_default: true },
          { environment_id: 'node', is_default: false },
        ]}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Default')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Make Node apps the default' }));
    expect(onChange).toHaveBeenCalledWith([
      { environment_id: 'go', is_default: false },
      { environment_id: 'node', is_default: true },
    ]);
  });

  it('shows a load failure instead of the list', () => {
    render(<EnvironmentsCard environments={[]} value={[]} onChange={vi.fn()} error="forbidden" />);
    expect(screen.getByText('Environments failed to load: forbidden')).toBeInTheDocument();
  });

  it('lists an attached environment that no longer exists so it can be cleared', () => {
    const onChange = vi.fn();
    render(
      <EnvironmentsCard
        environments={ENVIRONMENTS}
        value={[
          { environment_id: 'gone', is_default: true },
          { environment_id: 'go', is_default: false },
        ]}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByLabelText('Deleted environment'));
    expect(onChange).toHaveBeenCalledWith([{ environment_id: 'go', is_default: true }]);
  });
});
