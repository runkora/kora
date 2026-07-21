import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SaveBar } from './save-bar';

const noop = () => {};

describe('SaveBar', () => {
  it('renders nothing when neither visible nor success', () => {
    const { container } = render(<SaveBar visible={false} onSave={noop} onDiscard={noop} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows the dirty label and both actions when visible', () => {
    render(<SaveBar visible onSave={noop} onDiscard={noop} />);
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Discard' })).toBeEnabled();
  });

  it('fires onSave and onDiscard', () => {
    const onSave = vi.fn();
    const onDiscard = vi.fn();
    render(<SaveBar visible onSave={onSave} onDiscard={onDiscard} />);
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onDiscard).toHaveBeenCalledTimes(1);
  });

  it('disables both actions and swaps the label while saving', () => {
    render(<SaveBar visible saving onSave={noop} onDiscard={noop} />);
    expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Discard' })).toBeDisabled();
  });

  it('shows the success state without actions', () => {
    render(<SaveBar visible={false} success onSave={noop} onDiscard={noop} />);
    expect(screen.getByText('Changes saved')).toBeInTheDocument();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('renders the error message when provided', () => {
    render(<SaveBar visible error="Provisioning failed" onSave={noop} onDiscard={noop} />);
    expect(screen.getByText('Provisioning failed')).toHaveClass('text-status-error');
  });

  it('honours custom labels', () => {
    render(
      <SaveBar
        visible
        onSave={noop}
        onDiscard={noop}
        dirtyLabel="Pending edits"
        saveLabel="Apply"
        discardLabel="Revert"
      />,
    );
    expect(screen.getByText('Pending edits')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Apply' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Revert' })).toBeInTheDocument();
  });

  it('pins itself to the bottom-centre of the viewport', () => {
    render(<SaveBar visible onSave={noop} onDiscard={noop} />);
    const bar = document.querySelector('[data-slot="save-bar"]');
    expect(bar).toHaveClass('fixed', 'bottom-5', 'left-1/2', '-translate-x-1/2', 'z-40');
  });
});
