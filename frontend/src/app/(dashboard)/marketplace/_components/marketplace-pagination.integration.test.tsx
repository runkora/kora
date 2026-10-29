import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { MarketplaceProvider } from '../_context/marketplace-context';
import { MarketplacePagination } from './marketplace-pagination';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';

function browse(overrides: Partial<MarketplaceBrowseParams> = {}): MarketplaceBrowseParams {
  return { marketplace: 'Core', query: '', page: 1, pageSize: 24, sort: 'name-asc', ...overrides };
}

function overviewWith(page: number, pageSize: number, total: number): MarketplaceOverviewData {
  const b = browse({ page, pageSize });
  return {
    marketplaces: [
      { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
    ],
    requested: b,
    browse: b,
    skills: [],
    total,
  };
}

function renderPagination(data: MarketplaceOverviewData) {
  window.history.replaceState(null, '', '/marketplace');
  return renderWithProviders(
    <MarketplaceProvider initialData={data} initialError={null}>
      <MarketplacePagination />
    </MarketplaceProvider>,
  );
}

describe('result range', () => {
  it('shows the full range for a middle page', () => {
    renderPagination(overviewWith(2, 24, 100));
    expect(screen.getByText('25–48 of 100')).toBeInTheDocument();
  });

  it('shows a partial range for the last page', () => {
    renderPagination(overviewWith(5, 24, 100));
    expect(screen.getByText('97–100 of 100')).toBeInTheDocument();
  });

  it('shows 0–0 of 0 when there are no results', () => {
    renderPagination(overviewWith(1, 24, 0));
    expect(screen.getByText('0–0 of 0')).toBeInTheDocument();
  });
});

describe('page buttons', () => {
  it('shows every page with no gaps when the page count is small', () => {
    renderPagination(overviewWith(1, 24, 48)); // 2 pages
    expect(screen.getByRole('button', { name: 'Page 1' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('button', { name: 'Page 2' })).toBeInTheDocument();
  });

  it('shows gaps around the current page for a long result set', () => {
    renderPagination(overviewWith(7, 24, 480)); // 20 pages
    expect(screen.getByRole('button', { name: 'Page 1' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Page 5' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Page 7' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('button', { name: 'Page 9' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Page 20' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Page 3' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Page 15' })).not.toBeInTheDocument();
  });

  it('disables Previous on the first page', () => {
    renderPagination(overviewWith(1, 24, 100));
    expect(screen.getByRole('button', { name: /Previous/ })).toBeDisabled();
  });

  it('disables Next on the last page', () => {
    renderPagination(overviewWith(5, 24, 100));
    expect(screen.getByRole('button', { name: /Next/ })).toBeDisabled();
  });

  it('enables both buttons on a middle page', () => {
    renderPagination(overviewWith(2, 24, 100));
    expect(screen.getByRole('button', { name: /Previous/ })).toBeEnabled();
    expect(screen.getByRole('button', { name: /Next/ })).toBeEnabled();
  });
});
