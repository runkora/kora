import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { RadioGroup } from '../radio-group/radio-group';
import { RadioCard } from './radio-card';

function renderCards(onSelect: (value: string) => void, selected = 'a') {
  return render(
    <RadioGroup value={selected} aria-label="Options">
      <RadioCard
        value="a"
        selected={selected === 'a'}
        onSelect={onSelect}
        icon="square-terminal"
        title="Option A"
        description="First option."
      />
      <RadioCard value="b" selected={selected === 'b'} onSelect={onSelect} title="Option B" />
    </RadioGroup>,
  );
}

describe('RadioCard', () => {
  it('renders a radio control labelled by the title', () => {
    renderCards(vi.fn());
    expect(screen.getByRole('radio', { name: 'Option A' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Option B' })).toBeInTheDocument();
  });

  it('fires onSelect with the card value when the card surface is clicked', () => {
    const onSelect = vi.fn();
    renderCards(onSelect);
    fireEvent.click(screen.getByText('Option B'));
    expect(onSelect).toHaveBeenCalledWith('b');
  });

  it('marks the selected card with the Card selected state', () => {
    const { container } = renderCards(vi.fn(), 'a');
    const cards = container.querySelectorAll('[data-interactive="true"]');
    expect(cards).toHaveLength(2);
    expect(cards[0]).toHaveAttribute('data-selected', 'true');
    expect(cards[1]).not.toHaveAttribute('data-selected');
  });

  it('tints the icon tile accent only when selected', () => {
    const { container } = renderCards(vi.fn(), 'a');
    const tile = container.querySelector('[data-slot="icon-container"]');
    expect(tile).toHaveClass('bg-accent/15', 'text-accent');
  });

  it('renders the description when provided and omits it otherwise', () => {
    renderCards(vi.fn());
    expect(screen.getByText('First option.')).toBeInTheDocument();
  });
});
