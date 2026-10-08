/**
 * The shell is a pass-through, so the only thing worth asserting is that it
 * passes things through. initialDeliveryCursor is the one that matters: drop it
 * and the page renders with no cursor, which silently removes Load more and
 * caps the console at the first page again, which is the whole defect #412 is
 * about.
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';

// TriggersPage reads the router to refresh after a fire or a delete. jsdom has
// no app router mounted, so the hooks are stubbed the way the other component
// tests in this repo stub them.
vi.mock('next/navigation', () => ({
  useRouter: vi.fn().mockReturnValue({ refresh: vi.fn(), push: vi.fn() }),
  usePathname: vi.fn().mockReturnValue('/triggers'),
  useSearchParams: vi.fn().mockReturnValue(new URLSearchParams()),
}));

import { TriggersShell } from './triggers-shell';

function shell(cursor: string | null) {
  return (
    <TriggersShell
      sources={[]}
      triggers={[]}
      deliveries={[]}
      initialDeliveryCursor={cursor}
      agents={[]}
      initialError={null}
    />
  );
}

/** Load more lives on the Deliveries tab, which is not the default. */
async function openDeliveries() {
  await userEvent.click(screen.getByRole('tab', { name: 'Deliveries' }));
}

describe('TriggersShell', () => {
  it('forwards the cursor, so the page offers Load more', async () => {
    renderWithProviders(shell('2026-10-07T12:00:00Z|d1'));
    await openDeliveries();
    expect(await screen.findByRole('button', { name: 'Load more' })).toBeInTheDocument();
  });

  it('offers no Load more when there is no further page', async () => {
    renderWithProviders(shell(null));
    await openDeliveries();
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });
});
