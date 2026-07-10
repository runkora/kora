'use client';

/** @tier 3 — dev-environments-scoped. Client boundary holding the server-fetched data. */

import { DevEnvironmentsPage } from './dev-environments-page';
import type { DevEnvironment } from '@orbit/shared/api';

interface DevEnvironmentsShellProps {
  environments: DevEnvironment[];
  initialError: string | null;
}

export function DevEnvironmentsShell({ environments, initialError }: DevEnvironmentsShellProps) {
  return <DevEnvironmentsPage environments={environments} initialError={initialError} />;
}
