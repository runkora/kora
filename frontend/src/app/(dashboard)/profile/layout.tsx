// Shared layout for all profile pages.
// Provides consistent page padding — no data fetching, no providers.

import { Stack } from '@orbit/terra';

export default function ProfileLayout({ children }: { children: React.ReactNode }) {
  return (
    <Stack asChild padding="comfortable">
      <main>{children}</main>
    </Stack>
  );
}
