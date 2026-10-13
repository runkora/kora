import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SectionNav } from './section-nav';
import type { SectionNavSection } from './section-nav';

const SECTIONS: SectionNavSection[] = [
  { key: 'identity', title: 'Identity', sub: 'Name & description', icon: 'user' },
  { key: 'behaviour', title: 'Behaviour', sub: 'Prompt & model', icon: 'bot' },
  { key: 'tools', title: 'Tools & access', icon: 'wrench' },
];

describe('SectionNav', () => {
  it('renders one button per section', () => {
    render(<SectionNav sections={SECTIONS} active="identity" onSelect={vi.fn()} />);
    expect(screen.getAllByRole('button')).toHaveLength(3);
  });

  it('marks the active entry with aria-current', () => {
    render(<SectionNav sections={SECTIONS} active="behaviour" onSelect={vi.fn()} />);
    const active = screen.getByRole('button', { current: true });
    expect(active).toHaveTextContent('Behaviour');
  });

  it('shows an accent icon tile on the active entry and step dots elsewhere', () => {
    render(<SectionNav sections={SECTIONS} active="identity" onSelect={vi.fn()} />);
    const active = screen.getByRole('button', { current: true });
    expect(active.querySelector('[data-slot="icon-container"]')).not.toBeNull();
    const inactive = screen
      .getAllByRole('button')
      .filter((b) => b.getAttribute('aria-current') !== 'true');
    for (const button of inactive) {
      expect(button.querySelector('[data-slot="step-dot"]')).not.toBeNull();
    }
  });

  it('numbers inactive entries by 1-based index', () => {
    render(<SectionNav sections={SECTIONS} active="identity" onSelect={vi.fn()} />);
    const dots = document.querySelectorAll('[data-slot="step-dot"]');
    expect(Array.from(dots).map((d) => d.textContent)).toEqual(['2', '3']);
  });

  it('fires onSelect with the section key on click', () => {
    const onSelect = vi.fn();
    render(<SectionNav sections={SECTIONS} active="identity" onSelect={onSelect} />);
    fireEvent.click(screen.getByText('Tools & access'));
    expect(onSelect).toHaveBeenCalledWith('tools');
  });

  it('renders the sub line only when provided', () => {
    render(<SectionNav sections={SECTIONS} active="identity" onSelect={vi.fn()} />);
    expect(screen.getByText('Name & description')).toBeInTheDocument();
    expect(screen.getByText('Prompt & model')).toBeInTheDocument();
  });
});
