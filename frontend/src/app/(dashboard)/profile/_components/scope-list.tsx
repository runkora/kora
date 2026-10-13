/**
 * @tier 3 — profile-scoped. Promote to src/components/ if a third feature
 * needs a scope-chip list.
 */

import { Stack } from '@runkora/hades';
import { ScopeChip } from '@orbit/components';

interface ScopeListProps {
  scopes: string[];
}

export function ScopeList({ scopes }: ScopeListProps) {
  return (
    <Stack direction="horizontal" gap="tight" wrap>
      {scopes.map((s) => (
        <ScopeChip key={s} scope={s} />
      ))}
    </Stack>
  );
}
