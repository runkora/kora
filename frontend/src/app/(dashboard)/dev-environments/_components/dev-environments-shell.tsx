'use client';

/** @tier 3 — dev-environments-scoped. Client boundary holding the server-fetched data. */

import { DevEnvironmentsPage } from './dev-environments-page';
import type { DevEnvironment, DevEnvironmentLimits } from '@orbit/shared/api';

interface DevEnvironmentsShellProps {
  environments: DevEnvironment[];
  limits: DevEnvironmentLimits | null;
  initialError: string | null;
}

export function DevEnvironmentsShell({
  environments,
  limits,
  initialError,
}: DevEnvironmentsShellProps) {
  return (
    <DevEnvironmentsPage environments={environments} limits={limits} initialError={initialError} />
  );
}
