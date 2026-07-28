import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { DangerZone } from './danger-zone';

describe('DangerZone', () => {
  it('renders the title', () => {
    render(<DangerZone title="Delete this agent" />);
    expect(screen.getByText('Delete this agent')).toBeInTheDocument();
  });

  it('renders the description when provided', () => {
    render(<DangerZone title="Delete this agent" description="This cannot be undone." />);
    expect(screen.getByText('This cannot be undone.')).toBeInTheDocument();
  });

  it('renders the action slot and leaves its behaviour to the caller', () => {
    const onDelete = vi.fn();
    render(
      <DangerZone
        title="Delete this agent"
        action={
          <button type="button" onClick={onDelete}>
            Delete
          </button>
        }
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  it('omits the action wrapper when no action is supplied', () => {
    const { container } = render(<DangerZone title="Delete this agent" />);
    expect(container.querySelector('[data-slot="danger-zone-action"]')).toBeNull();
  });

  it('tints the container and the title with the error token', () => {
    const { container } = render(<DangerZone title="Delete this agent" />);
    const zone = container.querySelector('[data-slot="danger-zone"]');
    expect(zone).toHaveClass('border-status-error/30', 'bg-status-error/[0.07]', 'justify-between');
    expect(screen.getByText('Delete this agent')).toHaveClass('text-status-error');
  });

  it('merges a caller className and forwards the ref', () => {
    const ref = { current: null as HTMLDivElement | null };
    const { container } = render(<DangerZone ref={ref} className="mt-6" title="Delete" />);
    expect(container.querySelector('[data-slot="danger-zone"]')).toHaveClass('mt-6');
    expect(ref.current).toBeInstanceOf(HTMLElement);
  });
});
