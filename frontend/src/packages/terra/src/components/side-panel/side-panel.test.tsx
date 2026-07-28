import type { ReactNode } from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SidePanel, SidePanelHeader, SidePanelBody } from './side-panel';

function renderPanel(children: ReactNode) {
  return render(
    <SidePanel open onClose={() => {}}>
      {children}
    </SidePanel>,
  );
}

describe('SidePanelHeader', () => {
  it('renders the title and description', () => {
    renderPanel(<SidePanelHeader title="Edit model" description="Update the config." />);
    expect(screen.getByText('Edit model')).toBeInTheDocument();
    expect(screen.getByText('Update the config.')).toBeInTheDocument();
  });

  it('renders nothing for the leading slot when omitted', () => {
    renderPanel(<SidePanelHeader title="Edit model" />);
    expect(
      document.querySelector('[data-slot="side-panel-header-leading"]'),
    ).not.toBeInTheDocument();
  });

  it('renders the leading slot before the title block when provided', () => {
    renderPanel(<SidePanelHeader title="hermes-1" leading={<span>avatar</span>} />);
    const slot = document.querySelector('[data-slot="side-panel-header-leading"]');
    expect(slot).toBeInTheDocument();
    expect(slot).toHaveTextContent('avatar');
    expect(slot).toHaveClass('shrink-0');
  });

  it('keeps default density padding when density is not set', () => {
    renderPanel(<SidePanelHeader title="Edit model" />);
    const header = document.querySelector('[data-slot="side-panel-header"]');
    expect(header).toHaveClass('items-start', 'px-5', 'py-4');
    expect(header).not.toHaveClass('h-[60px]');
  });

  it('applies the compact density rail', () => {
    renderPanel(<SidePanelHeader density="compact" title="hermes-1" description="Running" />);
    const header = document.querySelector('[data-slot="side-panel-header"]');
    expect(header).toHaveClass('h-[60px]', 'items-center', 'px-[18px]', 'gap-3');
    expect(header).not.toHaveClass('px-5');
  });

  it('renders a compact title in mono at 14px and the description at 11.5px', () => {
    renderPanel(<SidePanelHeader density="compact" title="hermes-1" description="Running" />);
    const title = screen.getByText('hermes-1');
    expect(title).toHaveClass('font-mono', 'text-[14px]', 'font-semibold');
    expect(title).not.toHaveClass('font-sans');
    expect(screen.getByText('Running')).toHaveClass('text-[11.5px]');
  });
});

describe('SidePanelBody', () => {
  it('keeps default padding when padding is not set', () => {
    renderPanel(
      <>
        <SidePanelHeader title="Panel" />
        <SidePanelBody>content</SidePanelBody>
      </>,
    );
    const body = document.querySelector('[data-slot="side-panel-body"]');
    expect(body).toHaveClass('px-5', 'py-4');
  });

  it('applies 18px on all sides when padding is compact', () => {
    renderPanel(
      <>
        <SidePanelHeader title="Panel" />
        <SidePanelBody padding="compact">content</SidePanelBody>
      </>,
    );
    const body = document.querySelector('[data-slot="side-panel-body"]');
    expect(body).toHaveClass('p-[18px]');
    expect(body).not.toHaveClass('px-5');
  });

  it('drops padding entirely when flush is set', () => {
    renderPanel(
      <>
        <SidePanelHeader title="Panel" />
        <SidePanelBody flush padding="compact">
          content
        </SidePanelBody>
      </>,
    );
    const body = document.querySelector('[data-slot="side-panel-body"]');
    expect(body).not.toHaveClass('p-[18px]');
    expect(body).not.toHaveClass('px-5');
  });
});
