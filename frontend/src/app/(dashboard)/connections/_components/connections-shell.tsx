'use client';

/** @tier 3 — connections-scoped. Client boundary that holds the server-fetched rows. */

import { ConnectionsPage } from './connections-page';
import type { ConnectionRow } from '../_types';

interface ConnectionsShellProps {
  initialRows: ConnectionRow[];
  initialError: string | null;
  usedByError?: string | null;
}

export function ConnectionsShell({
  initialRows,
  initialError,
  usedByError = null,
}: ConnectionsShellProps) {
  return <ConnectionsPage rows={initialRows} error={initialError} usedByError={usedByError} />;
}
