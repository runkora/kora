import 'server-only';

import { listDevEnvironments } from '@orbit/server/devenvironments-client';
import type { DevEnvironment, DevEnvironmentLimits } from '@orbit/shared/api';

export type DevEnvironmentsResult =
  | { ok: true; environments: DevEnvironment[]; limits: DevEnvironmentLimits }
  | { ok: false; message: string };

/** Loads the caller's own dev environments server-side for the initial render. */
export async function fetchDevEnvironmentsPage(
  accessToken: string,
): Promise<DevEnvironmentsResult> {
  try {
    const { environments, limits } = await listDevEnvironments(accessToken);
    return { ok: true, environments, limits };
  } catch {
    return { ok: false, message: 'Failed to load dev environments' };
  }
}
