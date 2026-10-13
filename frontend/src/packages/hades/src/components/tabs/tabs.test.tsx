import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './tabs';

function renderTabs(props: { size?: 'default' | 'compact'; fill?: boolean } = {}) {
  return render(
    <Tabs defaultValue="one" {...props}>
      <TabsList>
        <TabsTrigger value="one">One</TabsTrigger>
        <TabsTrigger value="two">Two</TabsTrigger>
      </TabsList>
      <TabsContent value="one">First panel</TabsContent>
      <TabsContent value="two">Second panel</TabsContent>
    </Tabs>,
  );
}

describe('Tabs', () => {
  it('renders the active panel only', () => {
    renderTabs();
    expect(screen.getByText('First panel')).toBeInTheDocument();
    expect(screen.queryByText('Second panel')).not.toBeInTheDocument();
  });

  it('keeps default sizing when size is not set', () => {
    renderTabs();
    expect(screen.getByRole('tablist')).not.toHaveClass('px-[18px]');
    const trigger = screen.getByRole('tab', { name: 'One' });
    expect(trigger).toHaveClass('text-sm', 'px-4', 'pb-2.5', 'pt-2');
    expect(trigger).not.toHaveClass('h-[38px]');
  });

  it('insets the list and tightens the gap when size is compact', () => {
    renderTabs({ size: 'compact' });
    expect(screen.getByRole('tablist')).toHaveClass('px-[18px]', 'gap-[2px]');
  });

  it('renders 38px, 13px triggers with an accent underline when size is compact', () => {
    renderTabs({ size: 'compact' });
    const trigger = screen.getByRole('tab', { name: 'One' });
    expect(trigger).toHaveClass(
      'h-[38px]',
      'text-[13px]',
      'font-medium',
      '-mb-px',
      'border-b-2',
      'border-transparent',
      'data-[state=active]:font-semibold',
      'data-[state=active]:border-accent',
    );
    expect(trigger).not.toHaveClass('text-sm');
  });

  it('does not fill by default', () => {
    const { container } = renderTabs();
    expect(container.firstElementChild).not.toHaveClass('flex-1');
    expect(screen.getByRole('tabpanel')).not.toHaveClass('overflow-y-auto');
  });

  it('makes the root a filling column and scrolls the active panel when fill is set', () => {
    const { container } = renderTabs({ fill: true });
    expect(container.firstElementChild).toHaveClass('flex', 'flex-col', 'flex-1', 'min-h-0');
    expect(screen.getByRole('tabpanel')).toHaveClass('flex-1', 'min-h-0', 'overflow-y-auto');
  });
});
