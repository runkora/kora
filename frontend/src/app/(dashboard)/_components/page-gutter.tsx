/** @tier 3 — dashboard-shell-scoped. Promote to src/components/ if a second feature needs this. */
import { Stack } from '@runkora/hades';

/** The one content gutter for every dashboard page. */
export function PageGutter({ children }: { children: React.ReactNode }) {
  return <Stack padding="comfortable">{children}</Stack>;
}
