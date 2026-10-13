'use client';

/** @tier 3 — dashboard-scoped. Light/dark theme toggle via next-themes. */
import { useTheme } from 'next-themes';
import { Button, Icon } from '@clear-route/hades';

// Both buttons render; globals.css shows the one matching [data-theme] so the
// control is correct at first paint without waiting for hydration.
export function ThemeToggle() {
  const { setTheme } = useTheme();

  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Switch to dark theme"
        data-theme-switch="light"
        onClick={() => setTheme('dark')}
      >
        <Icon name="sun" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Switch to light theme"
        data-theme-switch="dark"
        onClick={() => setTheme('light')}
      >
        <Icon name="moon" />
      </Button>
    </>
  );
}
