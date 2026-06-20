import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PageLayout } from './page-layout';

/**
 * The responsive behaviour under test: when a `mobileDrawer` is supplied, the
 * fixed `<aside>` sidebar is hidden below `md` (the drawer takes over), and the
 * drawer slot is rendered. Without a drawer, the sidebar stays visible at all
 * widths. The drawer slot is inert unless a `sidebar` is also provided.
 */
describe('PageLayout', () => {
  it('renders its children', () => {
    render(<PageLayout>main content</PageLayout>);
    expect(screen.getByText('main content')).toBeInTheDocument();
  });

  it('keeps the fixed sidebar visible when no mobileDrawer is provided', () => {
    render(<PageLayout sidebar="SIDEBAR">main</PageLayout>);
    const aside = screen.getByRole('complementary');
    expect(aside).toHaveClass('flex');
    expect(aside).not.toHaveClass('hidden');
  });

  it('hides the fixed sidebar below md and renders the drawer when a mobileDrawer is provided', () => {
    render(
      <PageLayout sidebar="SIDEBAR" mobileDrawer="DRAWER">
        main
      </PageLayout>,
    );
    const aside = screen.getByRole('complementary');
    // `hidden md:flex` — sidebar collapses on mobile, reappears at md+
    expect(aside).toHaveClass('hidden');
    expect(aside).toHaveClass('md:flex');
    expect(screen.getByText('DRAWER')).toBeInTheDocument();
  });

  it('does not render the mobileDrawer slot when there is no sidebar', () => {
    render(<PageLayout mobileDrawer="DRAWER">main</PageLayout>);
    expect(screen.queryByText('DRAWER')).not.toBeInTheDocument();
  });
});
