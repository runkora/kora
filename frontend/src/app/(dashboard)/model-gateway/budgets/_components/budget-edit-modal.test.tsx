import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { BudgetAssignment } from '../../../policy/_types/api';
import { BudgetEditModal } from './budget-edit-modal';

function renderModal(
  kind: 'role' | 'grant',
  initialBudget: BudgetAssignment | null = null,
  onSave = vi.fn().mockResolvedValue({ ok: true }),
) {
  render(
    <BudgetEditModal
      open
      kind={kind}
      subject="x"
      initialBudget={initialBudget}
      onClose={vi.fn()}
      onSave={onSave}
    />,
  );
  return onSave;
}

const save = () => fireEvent.click(screen.getByRole('button', { name: 'Save Budget' }));

describe('BudgetEditModal', () => {
  it('has a per-run limit for a grant', () => {
    renderModal('grant');
    expect(screen.getByText('Per-Run Limit')).toBeInTheDocument();
    expect(screen.getByText('Window Limit')).toBeInTheDocument();
  });

  it('has no per-run limit for a role, and keeps its pooled toggle', () => {
    renderModal('role');
    expect(screen.queryByText('Per-Run Limit')).toBeNull();
    expect(screen.getByText('Shared Pool')).toBeInTheDocument();
  });

  it('shows an existing per-run limit and keeps it when only the window limit changes', async () => {
    const onSave = renderModal('grant', {
      limit_usd: 50,
      reset_cadence: 'monthly',
      per_run_limit_usd: 5,
    });
    expect(screen.getByPlaceholderText('e.g. 5')).toHaveValue(5);
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '80' } });
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        reset_cadence: 'monthly',
        limit_usd: 80,
        per_run_limit_usd: 5,
      }),
    );
  });

  it('saves a per-run-only grant budget', async () => {
    const onSave = renderModal('grant');
    expect(screen.getByRole('button', { name: 'Save Budget' })).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText('e.g. 5'), { target: { value: '3' } });
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({ reset_cadence: 'monthly', per_run_limit_usd: 3 }),
    );
  });

  it('saves a window-only grant budget without a per-run limit', async () => {
    const onSave = renderModal('grant');
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '30' } });
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({ reset_cadence: 'monthly', limit_usd: 30 }),
    );
  });

  it('needs a window limit for a role', () => {
    renderModal('role');
    expect(screen.getByRole('button', { name: 'Save Budget' })).toBeDisabled();
  });
});
