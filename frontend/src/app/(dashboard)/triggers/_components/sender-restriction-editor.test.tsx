import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SenderRestrictionEditor } from './sender-restriction-editor';

const AVAILABLE = ['OWNER', 'MEMBER', 'COLLABORATOR'];

function renderEditor(associations: string[] = [], disabled = false) {
  const onChange = vi.fn();
  render(
    <SenderRestrictionEditor
      available={AVAILABLE}
      value={{ logins: '', associations }}
      onChange={onChange}
      disabled={disabled}
    />,
  );
  return onChange;
}

describe('SenderRestrictionEditor', () => {
  it('shows a card per relationship with its description', () => {
    renderEditor();
    expect(screen.getByText('Owner')).toBeInTheDocument();
    expect(screen.getByText('Member of the organisation')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Collaborator' })).not.toBeChecked();
  });

  it('toggles a relationship when its card is clicked', () => {
    const onChange = renderEditor();
    fireEvent.click(screen.getByText('Member of the organisation'));
    expect(onChange).toHaveBeenCalledWith({ logins: '', associations: ['MEMBER'] });
  });

  it('toggles once when the checkbox itself is clicked', () => {
    const onChange = renderEditor(['MEMBER']);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Member' }));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ logins: '', associations: [] });
  });

  it('highlights a card on hover and keyboard focus unless disabled', () => {
    renderEditor();
    expect(screen.getByText('Owner').closest('[data-interactive]')).not.toBeNull();
  });

  it('ignores card clicks while disabled', () => {
    const onChange = renderEditor([], true);
    fireEvent.click(screen.getByText('Owner'));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByText('Owner').closest('[data-interactive]')).toBeNull();
  });

  it('renders nothing when the provider reports no relationships', () => {
    const { container } = render(
      <SenderRestrictionEditor
        available={[]}
        value={{ logins: '', associations: [] }}
        onChange={vi.fn()}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
