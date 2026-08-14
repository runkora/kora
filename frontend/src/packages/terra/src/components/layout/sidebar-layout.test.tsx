import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SidebarLayout } from './sidebar-layout';

// ── resolveSidebarWidth + buildGridTemplateColumns (via rendered style) ────────

describe('SidebarLayout grid layout', () => {
  it('resolves a SidebarWidthToken to its pixel value', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth="detail-panel">
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gridTemplateColumns).toBe('1fr 372px');
  });

  it('accepts a numeric width and appends px', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={300}>
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gridTemplateColumns).toBe('1fr 300px');
  });

  it('passes through an arbitrary string width verbatim', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth="20rem">
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gridTemplateColumns).toBe('1fr 20rem');
  });

  it('places the sidebar on the left when sidebarPosition="start"', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth="detail-panel" sidebarPosition="start">
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
        <SidebarLayout.Main>main</SidebarLayout.Main>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gridTemplateColumns).toBe('372px 1fr');
  });

  it('defaults sidebarPosition to end', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={200}>
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gridTemplateColumns).toBe('1fr 200px');
  });
});

// ── Gap resolution ─────────────────────────────────────────────────────────────

describe('SidebarLayout gap', () => {
  it('emits no gap style when gap is omitted', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={200}>
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gap).toBe('');
  });

  it('resolves relaxed to 24px', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={200} gap="relaxed">
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gap).toBe('24px');
  });

  it('resolves comfortable to 16px', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={200} gap="comfortable">
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gap).toBe('16px');
  });
});

// ── data-slot attributes ───────────────────────────────────────────────────────

describe('SidebarLayout data-slot', () => {
  it('sets data-slot on root, Main, and Sidebar', () => {
    render(
      <SidebarLayout sidebarWidth={200}>
        <SidebarLayout.Main>main content</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar content</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    expect(document.querySelector('[data-slot="sidebar-layout"]')).toBeTruthy();
    expect(document.querySelector('[data-slot="sidebar-layout-main"]')).toBeTruthy();
    expect(document.querySelector('[data-slot="sidebar-layout-sidebar"]')).toBeTruthy();
  });
});

// ── align ──────────────────────────────────────────────────────────────────────

describe('SidebarLayout align', () => {
  it('defaults to align-items: start', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={200}>
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.alignItems).toBe('start');
  });

  it('passes align prop through to alignItems', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={200} align="stretch">
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.alignItems).toBe('stretch');
  });
});

// ── style override ─────────────────────────────────────────────────────────────

describe('SidebarLayout style override', () => {
  it('merges user-supplied style after computed grid values', () => {
    const { container } = render(
      <SidebarLayout sidebarWidth={200} style={{ backgroundColor: 'red' }}>
        <SidebarLayout.Main>main</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.backgroundColor).toBe('red');
    expect(root.style.display).toBe('grid');
  });
});

// ── children render ────────────────────────────────────────────────────────────

describe('SidebarLayout renders children', () => {
  it('renders Main and Sidebar content', () => {
    render(
      <SidebarLayout sidebarWidth="detail-panel">
        <SidebarLayout.Main>main content</SidebarLayout.Main>
        <SidebarLayout.Sidebar>sidebar content</SidebarLayout.Sidebar>
      </SidebarLayout>,
    );
    expect(screen.getByText('main content')).toBeInTheDocument();
    expect(screen.getByText('sidebar content')).toBeInTheDocument();
  });
});
