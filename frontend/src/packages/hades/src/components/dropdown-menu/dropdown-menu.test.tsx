import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from './dropdown-menu';

describe('DropdownMenu', () => {
  it('renders its items as menu items, and a link through asChild', () => {
    render(
      <DropdownMenu open>
        <DropdownMenuTrigger>More</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem asChild>
            <a href="https://example.com/trace">Trace</a>
          </DropdownMenuItem>
          <DropdownMenuItem tone="danger">Delete</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>,
    );
    expect(screen.getByRole('menu')).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Trace' }).getAttribute('href')).toBe(
      'https://example.com/trace',
    );
    expect(screen.getByRole('menuitem', { name: 'Delete' }).className).toContain(
      'text-status-error',
    );
  });
});
