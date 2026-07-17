import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from './collapsible';

describe('Collapsible', () => {
  it('hides content by default and shows it after the trigger is clicked', () => {
    render(
      <Collapsible>
        <CollapsibleTrigger>Toggle</CollapsibleTrigger>
        <CollapsibleContent>Hidden tools</CollapsibleContent>
      </Collapsible>,
    );
    expect(screen.queryByText('Hidden tools')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Toggle' }));
    expect(screen.getByText('Hidden tools')).toBeInTheDocument();
  });

  it('shows content when defaultOpen is set and collapses on click', () => {
    render(
      <Collapsible defaultOpen>
        <CollapsibleTrigger>Toggle</CollapsibleTrigger>
        <CollapsibleContent>Visible tools</CollapsibleContent>
      </Collapsible>,
    );
    expect(screen.getByText('Visible tools')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Toggle' }));
    expect(screen.queryByText('Visible tools')).not.toBeInTheDocument();
  });

  it('wires aria-expanded on the trigger', () => {
    render(
      <Collapsible>
        <CollapsibleTrigger>Toggle</CollapsibleTrigger>
        <CollapsibleContent>Content</CollapsibleContent>
      </Collapsible>,
    );
    const trigger = screen.getByRole('button', { name: 'Toggle' });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
  });

  it('applies data-slot attributes', () => {
    const { container } = render(
      <Collapsible defaultOpen>
        <CollapsibleTrigger>Toggle</CollapsibleTrigger>
        <CollapsibleContent>Content</CollapsibleContent>
      </Collapsible>,
    );
    expect(container.querySelector('[data-slot="collapsible"]')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Toggle' })).toHaveAttribute(
      'data-slot',
      'collapsible-trigger',
    );
    expect(container.querySelector('[data-slot="collapsible-content"]')).toBeInTheDocument();
  });
});
