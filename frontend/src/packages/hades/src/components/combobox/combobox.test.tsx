import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Combobox, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList } from './combobox';

const PLANETS = [
  { value: 'mercury', keywords: ['closest to the sun'] },
  { value: 'venus', keywords: ['hottest'] },
  { value: 'mars', keywords: ['red'], disabled: true },
];

function setup() {
  const onSelect = vi.fn();
  render(
    <Combobox label="Planet">
      <ComboboxInput />
      <ComboboxList label="Planets">
        {PLANETS.map((p) => (
          <ComboboxItem
            key={p.value}
            value={p.value}
            keywords={p.keywords}
            disabled={p.disabled}
            onSelect={() => onSelect(p.value)}
          >
            {p.value}
          </ComboboxItem>
        ))}
        <ComboboxEmpty>No planet matches.</ComboboxEmpty>
      </ComboboxList>
    </Combobox>,
  );
  return { onSelect, user: userEvent.setup() };
}

describe('Combobox', () => {
  it('renders a listbox of options and names the input and the list', () => {
    setup();
    expect(screen.getByRole('combobox', { name: 'Planet' })).toBeInTheDocument();
    expect(screen.getByRole('listbox', { name: 'Planets' })).toBeInTheDocument();
    expect(screen.getAllByRole('option')).toHaveLength(3);
  });

  it('filters on the value and the keywords, ignoring case', async () => {
    const { user } = setup();
    await user.type(screen.getByRole('combobox'), 'HOT');
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['venus']);
  });

  it('shows the empty message when nothing matches', async () => {
    const { user } = setup();
    await user.type(screen.getByRole('combobox'), 'pluto');
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.getByText('No planet matches.')).toBeInTheDocument();
  });

  it('picks the highlighted option with the arrow keys and Enter', async () => {
    const { onSelect, user } = setup();
    await user.click(screen.getByRole('combobox'));
    await user.keyboard('{ArrowDown}{Enter}');
    expect(onSelect).toHaveBeenCalledWith('venus');
  });

  it('keeps a disabled option in the list and does not pick it', async () => {
    const { onSelect, user } = setup();
    const mars = screen.getByRole('option', { name: 'mars' });
    expect(mars).toHaveAttribute('aria-disabled', 'true');
    await user.click(mars);
    expect(onSelect).not.toHaveBeenCalled();
  });
});
