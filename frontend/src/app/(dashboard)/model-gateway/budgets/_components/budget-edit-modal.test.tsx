import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BudgetEditModal } from './budget-edit-modal';

function renderModal(kind: 'role' | 'grant', onSave = vi.fn().mockResolvedValue({ ok: true })) {
  render(
    <BudgetEditModal
      open
      kind={kind}
      subject="x"
      initialBudget={null}
      onClose={vi.fn()}
      onSave={onSave}
    />,
  );
  return onSave;
}

describe('BudgetEditModal', () => {
  it('has no per-run limit for a grant', () => {
    renderModal('grant');
    expect(screen.queryByText('Per-Run Limit')).toBeNull();
    expect(screen.getByText('Window Limit')).toBeInTheDocument();
  });

  it('has no per-run limit for a role either, and keeps its pooled toggle', () => {
    renderModal('role');
    expect(screen.queryByText('Per-Run Limit')).toBeNull();
    expect(screen.getByText('Shared Pool')).toBeInTheDocument();
  });

  it('saves a grant budget as a window limit only', async () => {
    const onSave = renderModal('grant');
    expect(screen.getByRole('button', { name: 'Save Budget' })).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText('e.g. 50'), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save Budget' }));
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({ reset_cadence: 'monthly', limit_usd: 30 }),
    );
  });
});
