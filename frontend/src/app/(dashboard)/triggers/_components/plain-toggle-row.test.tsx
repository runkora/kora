import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { PlainToggleRow } from './plain-toggle-row';

function setup(disabled = false) {
  const onChange = vi.fn();
  render(
    <PlainToggleRow
      label="Suppress duplicates"
      description="Ignore replays."
      checked={false}
      onChange={onChange}
      disabled={disabled}
    />,
  );
  return onChange;
}

describe('PlainToggleRow', () => {
  it('toggles when the card body is clicked', () => {
    const onChange = setup();
    fireEvent.click(screen.getByText('Ignore replays.'));
    expect(onChange).toHaveBeenCalledExactlyOnceWith(true);
  });

  it('toggles exactly once when the switch itself is clicked', () => {
    const onChange = setup();
    fireEvent.click(screen.getByRole('switch', { name: 'Suppress duplicates' }));
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('does nothing when disabled', () => {
    const onChange = setup(true);
    fireEvent.click(screen.getByText('Ignore replays.'));
    fireEvent.click(screen.getByRole('switch', { name: 'Suppress duplicates' }));
    expect(onChange).not.toHaveBeenCalled();
  });
});
