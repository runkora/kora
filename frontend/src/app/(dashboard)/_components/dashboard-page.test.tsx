/**
 * Unit tests for SpendBudgetCard (exported from dashboard-page).
 *
 * Covers the empty-state branch and the colour-threshold logic that maps
 * tenant spend vs limit onto the Progress bar variant and the status badge.
 */
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { SpendBudgetCard } from './dashboard-page';
import type { TenantCapStatus } from '../model-gateway/_types';

function cap(overrides: Partial<TenantCapStatus> = {}): TenantCapStatus {
  return {
    limit_usd: 200,
    reset_cadence: 'monthly',
    spend_usd: 50,
    reached: false,
    ...overrides,
  };
}

/** The variant colour lives on the fill div nested inside the progressbar. */
function fillClass(): string {
  const bar = screen.getByRole('progressbar', { name: 'Tenant spend usage' });
  return bar.querySelector('div')?.className ?? '';
}

describe('SpendBudgetCard', () => {
  describe('empty state', () => {
    it('shows the empty state when no cap is configured', () => {
      renderWithProviders(<SpendBudgetCard cap={null} />);

      expect(screen.getByText('No tenant budget configured.')).toBeInTheDocument();
      expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    });

    it('shows the empty state when the limit is zero', () => {
      renderWithProviders(<SpendBudgetCard cap={cap({ limit_usd: 0 })} />);

      expect(screen.getByText('No tenant budget configured.')).toBeInTheDocument();
    });
  });

  describe('configured budget', () => {
    it('renders spend and limit formatted as currency', () => {
      renderWithProviders(<SpendBudgetCard cap={cap({ spend_usd: 50, limit_usd: 200 })} />);

      expect(screen.getByText('$50.00')).toBeInTheDocument();
      expect(screen.getByText('of $200.00')).toBeInTheDocument();
    });

    it('exposes spend and limit on the progressbar with the rounded percentage', () => {
      renderWithProviders(<SpendBudgetCard cap={cap({ spend_usd: 50, limit_usd: 200 })} />);

      const bar = screen.getByRole('progressbar', { name: 'Tenant spend usage' });
      expect(bar).toHaveAttribute('aria-valuenow', '50');
      expect(bar).toHaveAttribute('aria-valuemax', '200');
      expect(screen.getByText('25%')).toBeInTheDocument();
    });

    it('shows the reset cadence', () => {
      renderWithProviders(<SpendBudgetCard cap={cap({ reset_cadence: 'weekly' })} />);

      expect(screen.getByText('Resets weekly')).toBeInTheDocument();
    });
  });

  describe('threshold variants', () => {
    it('uses the healthy variant below 80% utilisation', () => {
      renderWithProviders(<SpendBudgetCard cap={cap({ spend_usd: 100, limit_usd: 200 })} />);

      expect(fillClass()).toContain('bg-status-healthy');
      expect(screen.getByText('Within budget')).toBeInTheDocument();
    });

    it('uses the degraded variant at or above 80% utilisation', () => {
      renderWithProviders(<SpendBudgetCard cap={cap({ spend_usd: 160, limit_usd: 200 })} />);

      expect(fillClass()).toContain('bg-status-degraded');
      expect(screen.getByText('Within budget')).toBeInTheDocument();
    });

    it('uses the error variant and "Limit reached" when the cap is reached', () => {
      renderWithProviders(
        <SpendBudgetCard cap={cap({ spend_usd: 200, limit_usd: 200, reached: true })} />,
      );

      expect(fillClass()).toContain('bg-status-error');
      expect(screen.getByText('Limit reached')).toBeInTheDocument();
    });
  });
});
