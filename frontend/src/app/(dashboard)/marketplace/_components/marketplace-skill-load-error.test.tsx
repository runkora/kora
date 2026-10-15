import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MarketplaceSkillLoadError } from './marketplace-skill-load-error';

describe('MarketplaceSkillLoadError', () => {
  it('shows the failed-load notice and a Try again button', () => {
    render(<MarketplaceSkillLoadError onRetry={vi.fn()} />);

    expect(
      screen.getByText('Orbit could not load this Skill from the Marketplace.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled();
  });

  it('calls onRetry once for each click', async () => {
    const onRetry = vi.fn();
    render(<MarketplaceSkillLoadError onRetry={onRetry} />);

    await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }));

    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
