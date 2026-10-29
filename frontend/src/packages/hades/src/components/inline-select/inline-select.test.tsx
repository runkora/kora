import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { InlineSelect } from './inline-select';

const OPTIONS = [
  { value: 'is', label: 'is' },
  { value: 'contains', label: 'contains' },
  { value: 'starts', label: 'starts with' },
];

function setup(props: Partial<React.ComponentProps<typeof InlineSelect>> = {}) {
  const onValueChange = vi.fn();
  render(
    <InlineSelect
      aria-label="Operator"
      value="is"
      options={OPTIONS}
      onValueChange={onValueChange}
      {...props}
    />,
  );
  return { onValueChange, user: userEvent.setup() };
}

describe('InlineSelect', () => {
  it('shows the selected option as the word', () => {
    setup({ value: 'contains' });
    expect(screen.getByRole('button', { name: 'Operator' })).toHaveTextContent('contains');
  });

  it('shows the placeholder when the value matches no option', () => {
    setup({ value: 'nope', placeholder: 'Pick one' });
    expect(screen.getByRole('button', { name: 'Operator' })).toHaveTextContent('Pick one');
  });

  it('opens a listbox with the current option selected and focused', async () => {
    const { user } = setup({ value: 'contains' });
    await user.click(screen.getByRole('button', { name: 'Operator' }));
    expect(screen.getByRole('option', { name: 'contains' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByRole('option', { name: 'contains' })).toHaveFocus();
  });

  it('selects an option on click and closes', async () => {
    const { user, onValueChange } = setup();
    await user.click(screen.getByRole('button', { name: 'Operator' }));
    await user.click(screen.getByRole('option', { name: 'starts with' }));
    expect(onValueChange).toHaveBeenCalledWith('starts');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('selects the focused option on Enter', async () => {
    const { user, onValueChange } = setup();
    await user.click(screen.getByRole('button', { name: 'Operator' }));
    await user.keyboard('{ArrowDown}{Enter}');
    expect(onValueChange).toHaveBeenCalledWith('contains');
  });

  it('wraps focus around the ends with the arrow keys', async () => {
    const { user } = setup();
    await user.click(screen.getByRole('button', { name: 'Operator' }));
    await user.keyboard('{ArrowUp}');
    expect(screen.getByRole('option', { name: 'starts with' })).toHaveFocus();
    await user.keyboard('{ArrowDown}');
    expect(screen.getByRole('option', { name: 'is' })).toHaveFocus();
  });

  it('does not open when disabled', async () => {
    const { user } = setup({ disabled: true });
    await user.click(screen.getByRole('button', { name: 'Operator' }));
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });
});
