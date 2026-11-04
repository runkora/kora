import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import { AgentInstructionsCard } from './agent-instructions-card';

describe('AgentInstructionsCard', () => {
  it('counts the words and sends Edit to the caller', async () => {
    const onEdit = vi.fn();
    renderWithProviders(
      <AgentInstructionsCard prompt="Review the diff. Greet {{author}}." onEdit={onEdit} />,
    );
    expect(screen.getByText('5 words')).toBeTruthy();

    await userEvent.click(screen.getByRole('button', { name: 'Edit instructions' }));
    expect(onEdit).toHaveBeenCalledOnce();
  });

  it('shows the prompt as written in the Raw view', async () => {
    renderWithProviders(<AgentInstructionsCard prompt="# Title" onEdit={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: 'Raw' }));
    expect(screen.getByText('# Title')).toBeTruthy();
  });

  it('offers Edit when there are no instructions', () => {
    renderWithProviders(<AgentInstructionsCard prompt="" onEdit={vi.fn()} />);
    expect(screen.getByText('No instructions set')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Edit instructions' })).toBeTruthy();
  });

  it('disables Edit when the caller cannot edit here', () => {
    renderWithProviders(<AgentInstructionsCard prompt="# Title" onEdit={vi.fn()} editDisabled />);
    expect(screen.getByRole('button', { name: 'Edit instructions' })).toHaveProperty(
      'disabled',
      true,
    );
  });
});
