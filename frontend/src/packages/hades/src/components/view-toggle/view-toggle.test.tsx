import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { ViewToggle } from './view-toggle';

describe('ViewToggle', () => {
  it('reports the selected view and preserves controlled pressed states', () => {
    const onToggle = vi.fn();
    const ref = createRef<HTMLDivElement>();
    const { rerender } = render(<ViewToggle ref={ref} viewMode="cards" onToggle={onToggle} />);
    expect(ref.current).toHaveAttribute('data-slot', 'view-toggle');
    expect(screen.getByRole('button', { name: 'Card view' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    fireEvent.click(screen.getByRole('button', { name: 'List view' }));
    expect(onToggle).toHaveBeenCalledWith('list');
    rerender(<ViewToggle ref={ref} viewMode="list" onToggle={onToggle} />);
    expect(screen.getByRole('button', { name: 'List view' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: 'Card view' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Card view' }));
    expect(onToggle).toHaveBeenLastCalledWith('cards');
  });
});
