import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import type { SenderRole } from '@runkora/shared/api';
import { SenderRestrictionEditor } from './sender-restriction-editor';

// Labels and descriptions come from the provider's catalog, never from the console.
const ROLES: SenderRole[] = [
  { value: 'OWNER', label: 'Owner', description: 'Owns the repository' },
  { value: 'MEMBER', label: 'Member', description: 'Member of the organization' },
  { value: 'COLLABORATOR', label: 'Collaborator', description: 'Invited to the repository' },
];

function renderEditor(roles: string[] = [], disabled = false) {
  const onChange = vi.fn();
  render(
    <SenderRestrictionEditor
      identifiesSenders
      roles={ROLES}
      value={{ ids: '', roles }}
      onChange={onChange}
      disabled={disabled}
    />,
  );
  // Roles and ids sit behind "Specific senders"; open them when the value alone did not.
  if (screen.queryByLabelText('Owner') === null) {
    fireEvent.click(screen.getByRole('radio', { name: 'Specific senders' }));
  }
  return onChange;
}

describe('SenderRestrictionEditor', () => {
  it('shows a card per role with the label and description the provider sent', () => {
    renderEditor();
    expect(screen.getByText('Owner')).toBeInTheDocument();
    expect(screen.getByText('Member of the organization')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Collaborator' })).not.toBeChecked();
  });

  it('shows whatever text a provider declares for its roles', () => {
    render(
      <SenderRestrictionEditor
        identifiesSenders
        roles={[{ value: 'external', label: 'Guest', description: 'From another workspace' }]}
        value={{ ids: '', roles: [] }}
        onChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Specific senders' }));
    expect(screen.getByText('Guest')).toBeInTheDocument();
    expect(screen.getByText('From another workspace')).toBeInTheDocument();
    expect(screen.queryByText('Owner')).toBeNull();
  });

  it('toggles a role by its value when its card is clicked', () => {
    const onChange = renderEditor();
    fireEvent.click(screen.getByText('Member of the organization'));
    expect(onChange).toHaveBeenCalledWith({ ids: '', roles: ['MEMBER'] });
  });

  it('toggles once when the checkbox itself is clicked', () => {
    const onChange = renderEditor(['MEMBER']);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Member' }));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ ids: '', roles: [] });
  });

  it('edits the sender ids', () => {
    const onChange = renderEditor();
    fireEvent.change(screen.getByLabelText('Additional senders'), { target: { value: 'octocat' } });
    expect(onChange).toHaveBeenCalledWith({ ids: 'octocat', roles: [] });
  });

  it('highlights a card on hover and keyboard focus unless disabled', () => {
    renderEditor();
    expect(screen.getByText('Owner').closest('[data-interactive]')).not.toBeNull();
  });

  it('ignores card clicks while disabled', () => {
    const onChange = renderEditor(['MEMBER'], true);
    fireEvent.click(screen.getByText('Owner'));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByText('Owner').closest('[data-interactive]')).toBeNull();
  });

  it('shows no roles or ids while anyone may start a run', () => {
    render(
      <SenderRestrictionEditor
        identifiesSenders
        roles={ROLES}
        value={{ ids: '', roles: [] }}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole('radio', { name: 'Anyone' })).toBeChecked();
    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(screen.queryByLabelText('Additional senders')).toBeNull();
  });

  it('starts on specific senders when the stored restriction has entries', () => {
    render(
      <SenderRestrictionEditor
        identifiesSenders
        roles={ROLES}
        value={{ ids: 'octocat', roles: [] }}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole('radio', { name: 'Specific senders' })).toBeChecked();
  });

  it('clears the restriction when anyone is chosen', () => {
    const onChange = vi.fn();
    render(
      <SenderRestrictionEditor
        identifiesSenders
        roles={ROLES}
        value={{ ids: 'octocat', roles: ['OWNER'] }}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Anyone' }));
    expect(onChange).toHaveBeenCalledWith({ ids: '', roles: [] });
  });

  it('summarises roles and listed senders together, collapsing a long list', () => {
    render(
      <SenderRestrictionEditor
        identifiesSenders
        roles={ROLES}
        value={{ ids: 'a, b, c, d, e', roles: ['OWNER', 'MEMBER'] }}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/Can start a run:/)).toHaveTextContent(
      'Can start a run: every Owner and Member, also a, b, c and 2 more',
    );
  });

  it('says anyone can start a run while specific senders has nothing chosen', () => {
    renderEditor();
    expect(screen.getByText('Nothing chosen yet, so anyone can start a run.')).toBeInTheDocument();
  });

  it('renders nothing when the provider identifies no sender', () => {
    const { container } = render(
      <SenderRestrictionEditor
        identifiesSenders={false}
        roles={ROLES}
        value={{ ids: '', roles: [] }}
        onChange={vi.fn()}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('offers sender ids without role cards when the provider reports none', () => {
    render(
      <SenderRestrictionEditor
        identifiesSenders
        roles={[]}
        value={{ ids: '', roles: [] }}
        onChange={vi.fn()}
        idPlaceholder="U012AB3CD"
      />,
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Specific senders' }));
    expect(screen.getByLabelText('Allowed senders')).toHaveAttribute('placeholder', 'U012AB3CD');
    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(screen.queryByText(/role/)).toBeNull();
  });
});
