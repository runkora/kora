import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, within } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { EnvironmentsPage } from './environments-page';
import type { EnvironmentRow } from '../_types';

const refresh = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh }) }));

const deleteEnvironment = vi.fn();
vi.mock('../_services/environments-api', () => ({
  deleteEnvironment: (id: string) => deleteEnvironment(id),
  createEnvironment: vi.fn(),
  updateEnvironment: vi.fn(),
  previewEnvironmentAccess: () => new Promise(() => {}),
}));

const row = (id: string, name: string, sandboxImage: string): EnvironmentRow => ({
  id,
  slug: id,
  name,
  description: '',
  sandboxImage,
  size: { cpu: '2', memory: '4Gi', disk: '10Gi' },
  egressHosts: [],
  connectors: [],
  createdAt: '',
  updatedAt: '',
});

const ROWS = [
  row('a', 'Go services', 'acme-go'),
  row('b', 'Plain', 'kora-base'),
  row('c', 'Ops', 'acme-ops'),
];
const IMAGES = [
  { name: 'kora-base', description: '', image: 'r/base', valid: true, warm: true },
  { name: 'acme-go', description: '', image: 'r/go', valid: true, warm: false },
  {
    name: 'acme-ops',
    description: '',
    image: 'r/ops',
    valid: false,
    problem: 'runs as root',
    warm: false,
  },
];

describe('EnvironmentsPage', () => {
  it('shows each image, its validity and the size', () => {
    renderWithProviders(
      <EnvironmentsPage rows={ROWS} connections={[]} images={IMAGES} sizes={null} error={null} />,
    );
    expect(screen.getByText('acme-go')).toBeInTheDocument();
    expect(screen.getByText('kora-base')).toBeInTheDocument();
    expect(screen.getByText('runs as root')).toBeInTheDocument();
    expect(screen.getAllByText('2 CPU · 4Gi memory · 10Gi disk')).toHaveLength(3);
  });

  it.each([
    [['agents:environments:read'], false],
    [['agents:environments:read', 'agents:environments:write'], true],
  ])('with %j shows write controls: %s', (scopes, shown) => {
    renderWithProviders(
      <EnvironmentsPage rows={ROWS} connections={[]} images={IMAGES} sizes={null} error={null} />,
      { scopes },
    );
    expect(!!screen.queryByRole('button', { name: /new environment/i })).toBe(shown);
    expect(!!screen.queryByRole('button', { name: `Delete ${ROWS[0]!.name}` })).toBe(shown);
  });

  it('shows each slug under its name', () => {
    renderWithProviders(
      <EnvironmentsPage
        rows={[{ ...ROWS[0]!, slug: 'go-services' }]}
        connections={[]}
        images={IMAGES}
        sizes={null}
        error={null}
      />,
    );
    expect(screen.getByText('Go services')).toBeInTheDocument();
    expect(screen.getByText('go-services')).toBeInTheDocument();
  });

  it('refreshes after deleting an unattached environment', async () => {
    deleteEnvironment.mockResolvedValue({ error: null });
    renderWithProviders(
      <EnvironmentsPage rows={ROWS} connections={[]} images={IMAGES} sizes={null} error={null} />,
    );
    fireEvent.click(screen.getByLabelText('Delete Plain'));
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete environment' }),
    );
    await vi.waitFor(() => expect(refresh).toHaveBeenCalled());
  });
});
