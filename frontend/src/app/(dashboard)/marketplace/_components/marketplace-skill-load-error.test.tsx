/**
 * The "pending" state of the retry button depends on a real Next.js RSC
 * round trip and is covered in e2e, not here.
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MarketplaceSkillLoadError } from './marketplace-skill-load-error';

const refreshMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ refresh: refreshMock }),
}));

describe('MarketplaceSkillLoadError', () => {
  it('shows the failed-load message and an enabled Try again button', () => {
    render(<MarketplaceSkillLoadError />);
    expect(
      screen.getByText('Orbit could not load this Skill from the Marketplace.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled();
  });

  it('calls router.refresh() when Try again is clicked', async () => {
    const user = userEvent.setup();
    render(<MarketplaceSkillLoadError />);

    await user.click(screen.getByRole('button', { name: 'Try again' }));

    expect(refreshMock).toHaveBeenCalledTimes(1);
  });
});
