/** @tier 3 · triggers-scoped. The state of a Discord source's Gateway session. Promote to src/components/ if a second feature needs this. */

import { StatusBadge, type KoraStatus } from '@runkora/components';
import type { SourceConnection } from '../_types';

const BADGE: Record<SourceConnection['state'], { status: KoraStatus; label: string }> = {
  waiting: { status: 'pending', label: 'Waiting for bot token' },
  connecting: { status: 'pending', label: 'Connecting' },
  connected: { status: 'healthy', label: 'Connected' },
  rejected: { status: 'error', label: 'Rejected' },
};

export function ConnectionBadge({ connection }: { connection: SourceConnection | undefined }) {
  if (connection === undefined) return null;
  const { status, label } = BADGE[connection.state];
  return <StatusBadge status={status} label={label} />;
}
