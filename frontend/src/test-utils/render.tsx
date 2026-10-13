/**
 * renderWithProviders
 *
 * Drop-in replacement for RTL's `render` that wraps the component under test
 * in any application-level context providers. Add providers here as the app
 * grows — every test that imports this helper gets them automatically.
 */
import React from 'react';
import { render, type RenderOptions, type RenderResult } from '@testing-library/react';
import { TooltipProvider } from '@orbit/hades';
import { PreferencesProvider } from '../app/(dashboard)/_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../app/(dashboard)/_preferences/preferences.schema';

function Providers({ children }: { children: React.ReactNode }): React.JSX.Element {
  // Add global providers here as the application grows.
  return (
    <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
      <TooltipProvider>{children}</TooltipProvider>
    </PreferencesProvider>
  );
}

export function renderWithProviders(
  ui: React.ReactElement,
  options?: Omit<RenderOptions, 'wrapper'>,
): RenderResult {
  return render(ui, { wrapper: Providers, ...options });
}
