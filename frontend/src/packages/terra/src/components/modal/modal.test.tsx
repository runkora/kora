import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Modal, ModalHeader } from './modal';

function renderHeader(props: Parameters<typeof ModalHeader>[0]) {
  return render(
    <Modal open onClose={() => undefined}>
      <ModalHeader {...props} />
    </Modal>,
  );
}

describe('ModalHeader titleAccessory', () => {
  it('keeps the title as the dialog heading, without the accessory in its name', () => {
    renderHeader({ title: 'automated-deployment-safety-reviewer', titleAccessory: <b>AGENT</b> });
    expect(
      screen.getByRole('heading', { name: 'automated-deployment-safety-reviewer' }),
    ).toBeInTheDocument();
    expect(screen.getByText('AGENT')).toBeInTheDocument();
  });

  it('flows the accessory inline after the title text so it follows a wrapped title', () => {
    renderHeader({ title: 'a very long title', titleAccessory: <b>AGENT</b> });
    const heading = screen.getByRole('heading');
    const accessory = screen.getByText('AGENT').parentElement as HTMLElement;
    // Same block, inline children: a wrapping title puts the accessory at the end of its last line.
    expect(heading.parentElement).toBe(accessory.parentElement);
    expect(heading).toHaveClass('inline');
    expect(accessory).toHaveClass('inline-block', 'align-middle');
    // Not a flex row, which would centre the accessory against every wrapped line.
    expect(heading.parentElement?.className).not.toContain('flex');
  });

  it('renders no accessory wrapper when none is given', () => {
    renderHeader({ title: 'Plain' });
    expect(screen.getByRole('heading').nextElementSibling).toBeNull();
  });
});
