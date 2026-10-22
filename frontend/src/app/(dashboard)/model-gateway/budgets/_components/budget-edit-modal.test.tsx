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
const spendSwitch = () => screen.getByRole('switch', { name: 'Spend limit' });
const perRunSwitch = () => screen.getByRole('switch', { name: 'Per-run limit' });
const spendInput = () => screen.getByLabelText('Spend limit in USD');
const perRunInput = () => screen.getByLabelText('Per-run limit in USD');

describe('BudgetEditModal', () => {
  it('has a spend limit and a per-run limit row for a grant, both off with no budget', () => {
    renderModal('grant');
    expect(spendSwitch()).not.toBeChecked();
    expect(perRunSwitch()).not.toBeChecked();
    expect(spendInput()).toBeInTheDocument();
    expect(perRunInput()).toBeInTheDocument();
  });

  it('has a spend limit and a shared pool for a role, and no per-run row', () => {
    renderModal('role');
    expect(screen.getByRole('switch', { name: 'Spend limit' })).toBeInTheDocument();
    expect(screen.getByRole('switch', { name: 'Shared pool' })).toBeInTheDocument();
    expect(screen.queryByRole('switch', { name: 'Per-run limit' })).toBeNull();
    expect(screen.queryByLabelText('Per-run limit in USD')).toBeNull();
  });

  it('disables Save and says why while no row is on', () => {
    renderModal('grant');
    expect(screen.getByRole('button', { name: 'Save Budget' })).toBeDisabled();
    expect(screen.getByText('Turn on a spend limit or a per-run limit.')).toBeInTheDocument();
  });

  it('asks for the spend limit on a role', () => {
    renderModal('role');
    expect(screen.getByRole('button', { name: 'Save Budget' })).toBeDisabled();
    expect(screen.getByText('Turn on the spend limit.')).toBeInTheDocument();
  });

  it('prefills the rows an existing budget has and leaves the others off', () => {
    renderModal('grant', { limit_usd: 50, reset_cadence: 'weekly', per_run_limit_usd: 5 });
    expect(spendSwitch()).toBeChecked();
    expect(perRunSwitch()).toBeChecked();
    expect(spendInput()).toHaveValue('50');
    expect(perRunInput()).toHaveValue('5');
  });

  it('keeps an existing per-run limit when only the spend limit changes', async () => {
    const onSave = renderModal('grant', {
      limit_usd: 50,
      reset_cadence: 'monthly',
      per_run_limit_usd: 5,
    });
    fireEvent.change(spendInput(), { target: { value: '80' } });
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        reset_cadence: 'monthly',
        limit_usd: 80,
        per_run_limit_usd: 5,
      }),
    );
  });

  it('keeps an existing spend limit when only the per-run limit changes', async () => {
    const onSave = renderModal('grant', {
      limit_usd: 50,
      reset_cadence: 'monthly',
      per_run_limit_usd: 5,
    });
    fireEvent.change(perRunInput(), { target: { value: '7' } });
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        reset_cadence: 'monthly',
        limit_usd: 50,
        per_run_limit_usd: 7,
      }),
    );
  });

  it('saves a per-run-only grant budget', async () => {
    const onSave = renderModal('grant');
    fireEvent.click(perRunSwitch());
    fireEvent.change(perRunInput(), { target: { value: '3' } });
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({ reset_cadence: 'monthly', per_run_limit_usd: 3 }),
    );
  });

  it('saves a spend-only grant budget without a per-run limit', async () => {
    const onSave = renderModal('grant');
    fireEvent.click(spendSwitch());
    fireEvent.change(spendInput(), { target: { value: '30' } });
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({ reset_cadence: 'monthly', limit_usd: 30 }),
    );
  });

  it('blocks Save for a row that is on with no amount or a negative one', () => {
    renderModal('grant');
    fireEvent.click(spendSwitch());
    expect(screen.getByRole('button', { name: 'Save Budget' })).toBeDisabled();
    expect(screen.getByText('Enter a spend limit above 0.')).toBeInTheDocument();
    fireEvent.change(spendInput(), { target: { value: '-4' } });
    expect(screen.getByRole('button', { name: 'Save Budget' })).toBeDisabled();
    expect(screen.getByText('Enter an amount above 0.')).toBeInTheDocument();
  });

  it('saves a role budget with the shared pool', async () => {
    const onSave = renderModal('role');
    fireEvent.click(spendSwitch());
    fireEvent.change(spendInput(), { target: { value: '200' } });
    fireEvent.click(screen.getByRole('switch', { name: 'Shared pool' }));
    save();
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        reset_cadence: 'monthly',
        limit_usd: 200,
        pooled: true,
      }),
    );
  });
});
