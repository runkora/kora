'use client';

/** @tier 3 — environments-scoped. Client boundary holding the server-fetched rows. */

import type { ConnectionCatalogEntry } from '@orbit/shared/api';
import { EnvironmentsPage } from './environments-page';
import type { EnvironmentRow } from '../_types';

interface EnvironmentsShellProps {
  initialRows: EnvironmentRow[];
  connections: ConnectionCatalogEntry[];
  initialError: string | null;
}

export function EnvironmentsShell({
  initialRows,
  connections,
  initialError,
}: EnvironmentsShellProps) {
  return <EnvironmentsPage rows={initialRows} connections={connections} error={initialError} />;
}
