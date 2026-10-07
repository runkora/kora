'use client';

/** @tier 3 — environments-scoped. Client boundary holding the server-fetched rows. */

import type { ConnectionCatalogEntry, EnvironmentSizes, SandboxImage } from '@orbit/shared/api';
import { EnvironmentsPage } from './environments-page';
import type { EnvironmentRow } from '../_types';

interface EnvironmentsShellProps {
  initialRows: EnvironmentRow[];
  connections: ConnectionCatalogEntry[];
  images: SandboxImage[] | null;
  sizes: EnvironmentSizes | null;
  initialError: string | null;
}

export function EnvironmentsShell({
  initialRows,
  connections,
  images,
  sizes,
  initialError,
}: EnvironmentsShellProps) {
  return (
    <EnvironmentsPage
      rows={initialRows}
      connections={connections}
      images={images}
      sizes={sizes}
      error={initialError}
    />
  );
}
