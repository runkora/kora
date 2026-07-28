import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SettingRow } from './setting-row';

describe('SettingRow', () => {
  it('renders the label and the value', () => {
    render(<SettingRow label="Default model" value="claude-sonnet-4-5" />);
    expect(screen.getByText('Default model')).toBeInTheDocument();
    expect(screen.getByText('claude-sonnet-4-5')).toBeInTheDocument();
  });

  it('renders the description when provided', () => {
    render(<SettingRow label="Timeout" description="Runs stop past this." value="15m" />);
    expect(screen.getByText('Runs stop past this.')).toBeInTheDocument();
  });

  it('omits the description node entirely when absent', () => {
    const { container } = render(<SettingRow label="Region" value="ap-southeast-2" />);
    expect(container.querySelectorAll('.text-\\[11\\.5px\\]')).toHaveLength(0);
  });

  it('accepts a ReactNode value', () => {
    render(<SettingRow label="Policy" value={<span data-testid="chip">enforced</span>} />);
    expect(screen.getByTestId('chip')).toBeInTheDocument();
  });

  it('carries the data-slot attributes and the recessed value styling', () => {
    const { container } = render(<SettingRow label="Region" value="ap-southeast-2" />);
    const row = container.querySelector('[data-slot="setting-row"]');
    expect(row).toHaveClass('flex', 'items-center', 'bg-background', 'border-border-subtle');
    const value = container.querySelector('[data-slot="setting-row-value"]');
    expect(value).toHaveClass('font-mono', 'bg-surface', 'shrink-0');
  });

  it('merges a caller className and forwards the ref', () => {
    const ref = { current: null as HTMLDivElement | null };
    const { container } = render(
      <SettingRow ref={ref} className="mt-4" label="Region" value="x" />,
    );
    expect(container.querySelector('[data-slot="setting-row"]')).toHaveClass('mt-4');
    expect(ref.current).toBeInstanceOf(HTMLElement);
  });
});
