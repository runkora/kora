// ── Display-layer types for the Dev Environments feature ──────────────────────
//
// A dev environment is an ephemeral, vcluster-backed Orbit install, provisioned
// via the identity service's /dev-environments API and reconciled by the
// operator's DevEnvironmentReconciler. The wire shape (DevEnvironment) already
// matches everything the list/detail UI needs, so the row type is a plain
// alias — display logic lives in the helpers below rather than a bespoke row
// shape.

import type { DevEnvironment, DevEnvironmentPhase } from '@orbit/shared/api';
import type { OrbitStatus } from '@orbit/components';

export type { DevEnvironment as DevEnvironmentRow } from '@orbit/shared/api';

/**
 * Total-TTL-from-creation presets offered by the Extend modal. ttlHardCapHours
 * comes from the list endpoint's `limits` object (Service.Limits on the
 * backend) — never hardcoded here, so the frontend can't silently drift from
 * whatever cap the backend actually enforces.
 */
export function ttlPresets(ttlHardCapHours: number): { label: string; hours: number }[] {
  const maxDays = Math.round(ttlHardCapHours / 24);
  return [
    { label: '4 hours', hours: 4 },
    { label: '24 hours', hours: 24 },
    { label: '48 hours', hours: 48 },
    { label: '72 hours', hours: 72 },
    { label: `${maxDays} day${maxDays === 1 ? '' : 's'} (max)`, hours: ttlHardCapHours },
  ];
}

/**
 * Phases the reconciler is still actively working through — worth polling.
 * Sleeping only counts while a wake action this UI itself triggered is
 * still pending: without this, clicking "wake" and having the UI stop
 * polling before the environment actually reaches Ready leaves the page
 * showing a stale "Sleeping" status until the next unrelated re-render.
 */
export function isInFlight(
  env: Pick<DevEnvironment, 'id' | 'phase'>,
  pendingWakeIds?: ReadonlySet<string>,
): boolean {
  if (env.phase === 'Pending' || env.phase === 'Provisioning') return true;
  if (env.phase === 'Sleeping' && pendingWakeIds?.has(env.id)) return true;
  return false;
}

/** Maps a dev environment's lifecycle phase to a StatusBadge tone. */
export function phaseStatus(phase: DevEnvironmentPhase): OrbitStatus {
  switch (phase) {
    case 'Ready':
      return 'healthy';
    case 'Sleeping':
      return 'idle';
    case 'Pending':
    case 'Provisioning':
      return 'pending';
    case 'Failed':
      return 'error';
    default:
      return 'unknown';
  }
}

/** Renders an ISO timestamp as a stable, timezone-explicit string. */
export function formatTimestamp(iso: string | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.toISOString().slice(0, 19).replace('T', ' ')} UTC`;
}

/** Renders a countdown to an ISO expiry timestamp, or "Expired" once past. */
export function formatCountdown(iso: string | undefined): string {
  if (!iso) return '—';
  const target = new Date(iso).getTime();
  if (Number.isNaN(target)) return '—';
  const deltaMs = target - Date.now();
  if (deltaMs <= 0) return 'Expired';
  const hours = Math.floor(deltaMs / (60 * 60 * 1000));
  const minutes = Math.floor((deltaMs % (60 * 60 * 1000)) / (60 * 1000));
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const remHours = hours % 24;
    return `${days}d ${remHours}h`;
  }
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}
