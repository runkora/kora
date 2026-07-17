import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SectionHeading } from './section-heading';

describe('SectionHeading', () => {
  it('renders the title as an h3', () => {
    render(<SectionHeading icon="user" title="Identity" description="Who this agent is." />);
    const heading = screen.getByRole('heading', { level: 3, name: 'Identity' });
    expect(heading).toBeInTheDocument();
  });

  it('renders the description when provided', () => {
    render(<SectionHeading icon="user" title="Identity" description="Who this agent is." />);
    expect(screen.getByText('Who this agent is.')).toBeInTheDocument();
  });

  it('omits the description when not provided', () => {
    const { container } = render(<SectionHeading icon="user" title="Identity" />);
    expect(container.querySelectorAll('p')).toHaveLength(0);
  });

  it('renders the icon in an accent container tile', () => {
    const { container } = render(<SectionHeading icon="user" title="Identity" />);
    const tile = container.querySelector('[data-slot="icon-container"]');
    expect(tile).not.toBeNull();
    expect(tile).toHaveClass('bg-accent/15', 'text-accent');
  });

  it('carries the data-slot attribute on the root', () => {
    const { container } = render(<SectionHeading icon="user" title="Identity" />);
    expect(container.querySelector('[data-slot="section-heading"]')).not.toBeNull();
  });
});
