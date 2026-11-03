import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Modal, ModalBody, ModalFooter, ModalHeader } from './modal';

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

describe('Modal positioning', () => {
  function panel(size: 'sm' | 'md' | 'lg' | 'builder') {
    render(
      <Modal open size={size} onClose={() => undefined}>
        <ModalHeader title="Title" />
      </Modal>,
    );
    return screen.getByRole('dialog');
  }

  it.each(['sm', 'md', 'lg'] as const)(
    'centres the %s size at every width instead of pinning it to the left edge',
    (size) => {
      const el = panel(size);
      expect(el).not.toHaveClass('inset-0');
      expect(el).toHaveClass('left-1/2', 'top-1/2', '-translate-x-1/2', '-translate-y-1/2');
    },
  );

  it('keeps the builder size full-screen below lg', () => {
    expect(panel('builder')).toHaveClass('inset-0', 'lg:inset-auto');
  });
});

describe('Modal overflow', () => {
  function renderTall(size: 'sm' | 'md' | 'lg') {
    render(
      <Modal open size={size} onClose={() => undefined}>
        <ModalHeader title="Title" />
        <ModalBody>
          <p>{'tall content '.repeat(500)}</p>
        </ModalBody>
        <ModalFooter>
          <button>Done</button>
        </ModalFooter>
      </Modal>,
    );
  }

  // jsdom does no layout, so these pin the classes that make it work. Real
  // layout (scrollHeight vs clientHeight, footer on screen) is checked in a
  // browser against the Overflow story in modal.stories.tsx.
  it.each(['sm', 'md', 'lg'] as const)(
    'caps the %s panel to the viewport and clips it, so the body is what scrolls',
    (size) => {
      renderTall(size);
      const panel = screen.getByRole('dialog');
      expect(panel).toHaveClass('max-h-[calc(100dvh-2rem)]', 'overflow-hidden', 'flex-col');
    },
  );

  it('lets the body shrink and scroll without the constrained prop', () => {
    renderTall('md');
    const body = document.querySelector('[data-slot="modal-body"]') as HTMLElement;
    expect(body).toHaveClass('min-h-0', 'overflow-y-auto');
    expect(body).not.toHaveClass('max-h-[60vh]');
  });

  it('keeps the header and footer from shrinking away', () => {
    renderTall('md');
    expect(document.querySelector('[data-slot="modal-header"]')).toHaveClass('shrink-0');
    expect(document.querySelector('[data-slot="modal-footer"]')).toHaveClass('shrink-0');
  });
});

describe('ModalBody flushY', () => {
  it('pads 4px vertically, offset by a negative margin, so a focus ring is not clipped by the scroll area', () => {
    render(
      <Modal open onClose={() => undefined}>
        <ModalHeader title="Title" />
        <ModalBody flushY>
          <input />
        </ModalBody>
      </Modal>,
    );
    expect(document.querySelector('[data-slot="modal-body"]')).toHaveClass('py-1', '-my-1');
  });
});
