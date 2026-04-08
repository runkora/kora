/**
 * renderWithProviders
 *
 * Drop-in replacement for RTL's `render` that wraps the component under test
 * in any application-level context providers. Add providers here as the app
 * grows — every test that imports this helper gets them automatically.
 *
 * Currently there are no global React providers in the Orbit app (no Redux
 * store, no React Query client, no i18n context), so this is a thin wrapper.
 * It exists so that adding a provider later is a one-line change here rather
 * than a change across every test file.
 */
import React from 'react';
import { render, type RenderOptions, type RenderResult } from '@testing-library/react';

function Providers({ children }: { children: React.ReactNode }): React.JSX.Element {
  // Add global providers here as the application grows.
  return <>{children}</>;
}

export function renderWithProviders(
  ui: React.ReactElement,
  options?: Omit<RenderOptions, 'wrapper'>,
): RenderResult {
  return render(ui, { wrapper: Providers, ...options });
}
