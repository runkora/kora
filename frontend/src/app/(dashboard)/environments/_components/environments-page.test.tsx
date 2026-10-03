import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { EnvironmentsPage } from './environments-page';
import type { EnvironmentRow } from '../_types';

const refresh = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh }) }));

const IN_USE =
  'This environment is attached to an agent. Detach it from every agent, then delete it.';
const deleteEnvironment = vi.fn();
vi.mock('../_services/environments-api', () => ({
  deleteEnvironment: (id: string) => deleteEnvironment(id),
  createEnvironment: vi.fn(),
  updateEnvironment: vi.fn(),
  previewEnvironmentAccess: () => new Promise(() => {}),
}));

const row = (id: string, name: string, sandboxImage: string): EnvironmentRow => ({
  id,
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
  row('b', 'Plain', ''),
  row('c', 'Ops', 'acme-ops'),
];
const IMAGES = [
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
    render(<EnvironmentsPage rows={ROWS} connections={[]} images={IMAGES} error={null} />);
    expect(screen.getByText('acme-go')).toBeInTheDocument();
    expect(screen.getByText('kora base image')).toBeInTheDocument();
    expect(screen.getByText('runs as root')).toBeInTheDocument();
    expect(screen.getAllByText('2 CPU · 4Gi memory · 10Gi disk')).toHaveLength(3);
  });

  it('explains why an attached environment cannot be deleted', async () => {
    deleteEnvironment.mockResolvedValue({ error: IN_USE });
    render(<EnvironmentsPage rows={ROWS} connections={[]} images={IMAGES} error={null} />);
    fireEvent.click(screen.getByLabelText('Delete Go services'));
    const dialog = within(screen.getByRole('dialog'));
    fireEvent.click(dialog.getByRole('button', { name: 'Delete environment' }));
    expect(await dialog.findByText(IN_USE)).toBeInTheDocument();
    expect(deleteEnvironment).toHaveBeenCalledWith('a');
    expect(refresh).not.toHaveBeenCalled();
  });

  it('refreshes after deleting an unattached environment', async () => {
    deleteEnvironment.mockResolvedValue({ error: null });
    render(<EnvironmentsPage rows={ROWS} connections={[]} images={IMAGES} error={null} />);
    fireEvent.click(screen.getByLabelText('Delete Plain'));
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete environment' }),
    );
    await vi.waitFor(() => expect(refresh).toHaveBeenCalled());
  });
});
