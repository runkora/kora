import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ToggleRow } from './toggle-row';

describe('ToggleRow (switch form)', () => {
  it('exposes a switch with the label as its accessible name', () => {
    render(<ToggleRow checked={false} onChange={vi.fn()} label="Streaming responses" />);
    expect(screen.getByRole('switch', { name: /Streaming responses/ })).toBeInTheDocument();
  });

  it('reflects checked state in aria-checked', () => {
    const { rerender } = render(<ToggleRow checked={false} onChange={vi.fn()} label="Streaming" />);
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false');
    rerender(<ToggleRow checked onChange={vi.fn()} label="Streaming" />);
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true');
  });

  it('renders the description when provided', () => {
    render(
      <ToggleRow
        checked
        onChange={vi.fn()}
        label="Streaming"
        description="Emit tokens incrementally."
      />,
    );
    expect(screen.getByText('Emit tokens incrementally.')).toBeInTheDocument();
  });

  it('fires onChange with the negated state when the row is clicked', () => {
    const onChange = vi.fn();
    render(<ToggleRow checked={false} onChange={onChange} label="Streaming" />);
    fireEvent.click(screen.getByRole('switch'));
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('treats the whole row as the click target, not just the track', () => {
    const onChange = vi.fn();
    render(<ToggleRow checked onChange={onChange} label="Streaming" description="Detail line." />);
    fireEvent.click(screen.getByText('Detail line.'));
    expect(onChange).toHaveBeenCalledWith(false);
  });

  it('is reachable by Tab and toggled by Space and Enter', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<ToggleRow checked={false} onChange={onChange} label="Streaming" />);

    await user.tab();
    expect(screen.getByRole('switch')).toHaveFocus();

    await user.keyboard('{ }');
    await user.keyboard('{Enter}');
    expect(onChange).toHaveBeenCalledTimes(2);
    expect(onChange).toHaveBeenNthCalledWith(1, true);
    expect(onChange).toHaveBeenNthCalledWith(2, true);
  });

  it('does not fire onChange or take focus when disabled', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<ToggleRow checked={false} onChange={onChange} label="Streaming" disabled />);

    const row = screen.getByRole('switch');
    expect(row).toBeDisabled();
    fireEvent.click(row);
    await user.tab();
    expect(row).not.toHaveFocus();
    expect(onChange).not.toHaveBeenCalled();
  });

  it('moves the knob and tints the track with the checked state', () => {
    const { container, rerender } = render(
      <ToggleRow checked={false} onChange={vi.fn()} label="Streaming" />,
    );
    const track = () => container.querySelector('[data-slot="toggle-row-track"]');
    const knob = () => track()?.firstElementChild;

    // The shared toggle-visuals recipe, the same one `Toggle` draws.
    expect(track()).toHaveClass('bg-surface-elevated', 'h-5', 'w-9');
    expect(knob()).toHaveClass('translate-x-0', 'transition-transform');

    rerender(<ToggleRow checked onChange={vi.fn()} label="Streaming" />);
    expect(track()).toHaveClass('bg-accent');
    expect(knob()).toHaveClass('translate-x-4');
  });

  it('recedes the text block while the setting is off', () => {
    const { container, rerender } = render(
      <ToggleRow checked={false} onChange={vi.fn()} label="Streaming" />,
    );
    const text = () => container.querySelector('[data-slot="toggle-row-text"]');
    expect(text()).toHaveClass('opacity-85');

    rerender(<ToggleRow checked onChange={vi.fn()} label="Streaming" />);
    expect(text()).toHaveClass('opacity-100');
  });

  it('merges a caller className onto the row', () => {
    render(<ToggleRow checked onChange={vi.fn()} label="Streaming" className="mt-4" />);
    expect(screen.getByRole('switch')).toHaveClass('mt-4', 'bg-background');
  });
});

describe('ToggleRow (composed form)', () => {
  it('renders the supplied control and associates the label with it', () => {
    render(
      <ToggleRow
        htmlFor="budget"
        label="Budget"
        description="Spend ceiling on this grant."
        control={<input id="budget" type="checkbox" />}
      />,
    );
    expect(screen.getByLabelText('Budget')).toBeInTheDocument();
    expect(screen.getByText('Spend ceiling on this grant.')).toBeInTheDocument();
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
  });

  it('dims and blocks pointer events when disabled', () => {
    const { container } = render(
      <ToggleRow label="Budget" disabled control={<input type="checkbox" />} />,
    );
    expect(container.querySelector('[data-slot="toggle-row"]')).toHaveClass(
      'opacity-50',
      'pointer-events-none',
    );
  });
});
