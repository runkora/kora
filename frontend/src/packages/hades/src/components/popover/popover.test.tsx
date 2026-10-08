import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Popover, PopoverContent, PopoverTrigger } from './popover';

function renderOpen(matchTriggerWidth?: boolean) {
  render(
    <Popover open>
      <PopoverTrigger>Open</PopoverTrigger>
      <PopoverContent matchTriggerWidth={matchTriggerWidth}>Body</PopoverContent>
    </Popover>,
  );
  return screen.getByText('Body');
}

describe('PopoverContent', () => {
  it('matches the trigger width when asked', () => {
    expect(renderOpen(true).className).toContain('w-[var(--radix-popover-trigger-width)]');
  });

  it('sizes to its content by default', () => {
    expect(renderOpen().className).not.toContain('--radix-popover-trigger-width');
  });
});
