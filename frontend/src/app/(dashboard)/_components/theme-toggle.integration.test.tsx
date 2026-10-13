/**
 * Integration tests for ThemeToggle.
 *
 * next-themes is mocked so setTheme calls can be observed. Both buttons render
 * in jsdom — the [data-theme]-driven CSS that shows exactly one of them lives
 * in globals.css, which jsdom does not apply.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { ThemeToggle } from './theme-toggle';

const setTheme = vi.fn();

vi.mock('next-themes', () => ({
  useTheme: () => ({ setTheme }),
}));

beforeEach(() => {
  setTheme.mockClear();
});

describe('ThemeToggle', () => {
  it('renders one button per theme, each labelled with its target', () => {
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole('button', { name: 'Switch to dark theme' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Switch to light theme' })).toBeInTheDocument();
  });

  it('marks each button with the theme it is visible under', () => {
    renderWithProviders(<ThemeToggle />);
    expect(screen.getByRole('button', { name: 'Switch to dark theme' })).toHaveAttribute(
      'data-theme-switch',
      'light',
    );
    expect(screen.getByRole('button', { name: 'Switch to light theme' })).toHaveAttribute(
      'data-theme-switch',
      'dark',
    );
  });

  it('the sun button (shown in light mode) switches to dark', async () => {
    renderWithProviders(<ThemeToggle />);
    await userEvent.click(screen.getByRole('button', { name: 'Switch to dark theme' }));
    expect(setTheme).toHaveBeenCalledExactlyOnceWith('dark');
  });

  it('the moon button (shown in dark mode) switches to light', async () => {
    renderWithProviders(<ThemeToggle />);
    await userEvent.click(screen.getByRole('button', { name: 'Switch to light theme' }));
    expect(setTheme).toHaveBeenCalledExactlyOnceWith('light');
  });
});
