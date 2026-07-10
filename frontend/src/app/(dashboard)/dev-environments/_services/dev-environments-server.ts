import 'server-only';

import { listDevEnvironments } from '@orbit/server/devenvironments-client';
import type { DevEnvironment } from '@orbit/shared/api';

export type DevEnvironmentsResult =
  | { ok: true; environments: DevEnvironment[] }
  | { ok: false; message: string };

/** Loads the caller's own dev environments server-side for the initial render. */
export async function fetchDevEnvironmentsPage(
  accessToken: string,
): Promise<DevEnvironmentsResult> {
  try {
    const environments = await listDevEnvironments(accessToken);
    return { ok: true, environments };
  } catch {
    return { ok: false, message: 'Failed to load dev environments' };
  }
}
