import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Avatar, initialsOf } from './avatar';

describe('initialsOf', () => {
  it.each([
    ['Dana Lee', 'DL'],
    ['dana', 'D'],
    ['Mary Ann de Vries', 'MV'],
    ['dana.lee@example.com', 'DC'],
    ['', ''],
  ])('%s is %s', (name, want) => {
    expect(initialsOf(name)).toBe(want);
  });
});

describe('Avatar', () => {
  it('shows the initials and is hidden from screen readers without a label', () => {
    const { container } = render(<Avatar name="Dana Lee" />);
    const el = container.querySelector('[data-slot="avatar"]');
    expect(el).toHaveTextContent('DL');
    expect(el).toHaveAttribute('aria-hidden', 'true');
  });

  it('is an image named by its label', () => {
    render(<Avatar name="Dana Lee" label="Dana Lee" />);
    expect(screen.getByRole('img', { name: 'Dana Lee' })).toHaveTextContent('DL');
  });
});
