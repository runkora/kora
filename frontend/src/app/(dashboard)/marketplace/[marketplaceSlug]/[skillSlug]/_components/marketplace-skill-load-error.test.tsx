'use client';

import { Suspense, use, useEffect, useState } from 'react';
import { beforeEach, describe, it, expect, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MarketplaceSkillLoadError } from './marketplace-skill-load-error';

const refreshMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ refresh: refreshMock }),
}));

function RefreshResult({ response }: { response: Promise<void> | null }) {
  if (response) use(response);
  return null;
}

/** Model refresh's suspending route update without mocking React's transition. */
function RefreshHarness({ response }: { response: Promise<void> }) {
  const [activeResponse, setActiveResponse] = useState<Promise<void> | null>(null);

  useEffect(() => {
    refreshMock.mockImplementation(() => setActiveResponse(response));
  }, [response]);

  return (
    <>
      <MarketplaceSkillLoadError />
      <Suspense fallback={null}>
        <RefreshResult response={activeResponse} />
      </Suspense>
    </>
  );
}

describe('MarketplaceSkillLoadError', () => {
  beforeEach(() => {
    refreshMock.mockReset();
  });

  it('shows the failed-load message and an enabled Try again button', () => {
    render(<MarketplaceSkillLoadError />);
    expect(
      screen.getByText('Orbit could not load this Skill from the Marketplace.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled();
  });

  it('refreshes once and disables retry until the pending refresh finishes', async () => {
    const user = userEvent.setup();
    let finishRefresh!: () => void;
    const response = new Promise<void>((resolve) => {
      finishRefresh = resolve;
    });
    render(<RefreshHarness response={response} />);
    const retry = screen.getByRole('button', { name: 'Try again' });

    expect(retry).toBeEnabled();
    await act(async () => {
      await user.click(retry);
    });

    expect(refreshMock).toHaveBeenCalledTimes(1);
    expect(retry).toBeDisabled();

    await act(async () => {
      await user.click(retry);
    });
    expect(refreshMock).toHaveBeenCalledTimes(1);
    expect(retry).toBeDisabled();

    await act(async () => {
      finishRefresh();
      await response;
    });

    expect(retry).toBeEnabled();
  });
});
