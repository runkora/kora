import { describe, expect, it, vi } from 'vitest';
import { APP_ROUTES } from '@runkora/lib/routing';

const redirect = vi.hoisted(() => vi.fn());
vi.mock('next/navigation', () => ({ redirect }));

import HomeRoute from './page';

describe('HomeRoute', () => {
  it('sends / to the Runs tab', () => {
    HomeRoute();
    expect(redirect).toHaveBeenCalledWith(APP_ROUTES.runs);
  });
});
