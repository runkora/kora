'use client';

/** @tier 3 — triggers-scoped. FieldRow's layout on the elevated step: Hades' FieldRow
 * always sits on the darkest one, and this modal wants its rows a shade lighter.
 * Promote to src/components/ if a second feature needs it. */

import type { ReactNode } from 'react';
import { Card, Stack } from '@runkora/hades';

export function PlainRow({
  leading,
  actions,
  children,
}: {
  /** Leading element: an icon, a status marker, or a short key. */
  leading?: ReactNode;
  /** Trailing actions or a short note. */
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Card subtle border="subtle">
      <Stack
        direction="horizontal"
        align="center"
        gap="cozy"
        paddingInline="cozy"
        paddingBlock="snug"
      >
        {leading}
        <Stack grow minWidth="none">
          {children}
        </Stack>
        {actions}
      </Stack>
    </Card>
  );
}
