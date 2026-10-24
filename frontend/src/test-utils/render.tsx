/**
 * renderWithProviders
 *
 * Drop-in replacement for RTL's `render` that wraps the component under test
 * in any application-level context providers. Add providers here as the app
 * grows — every test that imports this helper gets them automatically.
 */
import React from 'react';
import { render, type RenderOptions, type RenderResult } from '@testing-library/react';
import { TooltipProvider } from '@runkora/hades';
import { PreferencesProvider } from '../app/(dashboard)/_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../app/(dashboard)/_preferences/preferences.schema';
import { StaticPermissionsProvider } from '../app/(dashboard)/_components/permissions-provider';

/**
 * Two-part scopes cover every scope of the same type and action, so this set
 * passes every static scope gate. Tests of a gate pass their own narrower set.
 */
export const ALL_TEST_SCOPES = [
  'agents:read',
  'agents:write',
  'triggers:read',
  'triggers:write',
  'trigger:fire',
  'platform:read',
  'platform:write',
  'inference:list',
  'inference:use',
];

export function renderWithProviders(
  ui: React.ReactElement,
  options?: Omit<RenderOptions, 'wrapper'> & { scopes?: string[]; apiAuthoring?: boolean },
): RenderResult {
  const { scopes = ALL_TEST_SCOPES, apiAuthoring = true, ...rest } = options ?? {};
  function Providers({ children }: { children: React.ReactNode }): React.JSX.Element {
    return (
      <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
        <StaticPermissionsProvider scopes={scopes} apiAuthoring={apiAuthoring}>
          <TooltipProvider>{children}</TooltipProvider>
        </StaticPermissionsProvider>
      </PreferencesProvider>
    );
  }
  return render(ui, { wrapper: Providers, ...rest });
}
