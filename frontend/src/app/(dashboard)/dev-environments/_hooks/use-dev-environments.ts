'use client';

// ── useDevEnvironments ────────────────────────────────────────────────────────
//
// Owns all reactive state for the /dev-environments page: the list, the
// platform limits (TTL hard cap, per-owner quota), and every lifecycle action
// a row can trigger. Polls the list while any environment is still
// Pending/Provisioning, or Sleeping with a wake this hook itself triggered
// still pending — mirrors the agent-type detail page's own provisioning-poll
// pattern (agents/[slug]/_hooks/use-agent-detail.ts).

import { useCallback, useEffect, useRef, useState } from 'react';
import type {
  DevEnvironment,
  DevEnvironmentKubeconfigResponse,
  DevEnvironmentLimits,
} from '@orbit/shared/api';
import {
  createDevEnvironment,
  destroyDevEnvironment,
  extendDevEnvironment,
  fetchDevEnvironments,
  kubeconfigDevEnvironment,
  wakeDevEnvironment,
} from '../_services/dev-environments-api';
import { isInFlight } from '../_types';

const POLL_INTERVAL_MS = 3000;

export interface UseDevEnvironmentsShape {
  environments: DevEnvironment[];
  limits: DevEnvironmentLimits | null;
  error: string | null;
  refresh: () => Promise<void>;
  create: (ref: string) => Promise<{ error: string | null }>;
  extend: (id: string, ttl: string) => Promise<{ error: string | null }>;
  wake: (id: string) => Promise<{ error: string | null }>;
  destroy: (id: string) => Promise<{ error: string | null }>;
  kubeconfig: (
    id: string,
  ) => Promise<{ kubeconfig: DevEnvironmentKubeconfigResponse | null; error: string | null }>;
}

// Removes ids from a pending-wake set whose environment no longer reports
// Sleeping — the point at which a triggered wake is considered resolved.
// Returns the same reference when nothing changed, so callers can skip a
// state update (and therefore a re-render) on every ordinary poll tick.
function reconcilePendingWakes(
  prev: ReadonlySet<string>,
  envs: DevEnvironment[],
): ReadonlySet<string> {
  if (prev.size === 0) return prev;
  const stillSleeping = new Set(envs.filter((e) => e.phase === 'Sleeping').map((e) => e.id));
  const next = new Set([...prev].filter((id) => stillSleeping.has(id)));
  return next.size === prev.size ? prev : next;
}

export function useDevEnvironments(
  initial: DevEnvironment[],
  initialLimits: DevEnvironmentLimits | null,
  initialError: string | null,
): UseDevEnvironmentsShape {
  const [environments, setEnvironments] = useState<DevEnvironment[]>(initial);
  const [limits, setLimits] = useState<DevEnvironmentLimits | null>(initialLimits);
  const [error, setError] = useState<string | null>(initialError);
  const [pendingWakeIds, setPendingWakeIds] = useState<ReadonlySet<string>>(new Set());

  const applyList = useCallback((envs: DevEnvironment[]) => {
    setEnvironments(envs);
    setPendingWakeIds((prev) => reconcilePendingWakes(prev, envs));
  }, []);

  const refresh = useCallback(async () => {
    const result = await fetchDevEnvironments();
    if (result.error) {
      setError(result.error);
      return;
    }
    setError(null);
    applyList(result.data);
    if (result.limits) setLimits(result.limits);
  }, [applyList]);

  // Poll while any environment is still being provisioned, or is Sleeping
  // with a wake this hook triggered still pending.
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const hasInFlight = environments.some((e) => isInFlight(e, pendingWakeIds));
  useEffect(() => {
    if (!hasInFlight) return;
    pollingRef.current = setInterval(() => {
      void fetchDevEnvironments().then((result) => {
        if (!result.error) {
          applyList(result.data);
          if (result.limits) setLimits(result.limits);
        }
      });
    }, POLL_INTERVAL_MS);
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [hasInFlight, applyList]);

  const create = useCallback(async (ref: string): Promise<{ error: string | null }> => {
    const result = await createDevEnvironment({ ref: ref || undefined });
    if (result.error || !result.environment) {
      return { error: result.error ?? 'Failed to create dev environment.' };
    }
    setEnvironments((prev) => [result.environment!, ...prev]);
    return { error: null };
  }, []);

  const extend = useCallback(async (id: string, ttl: string): Promise<{ error: string | null }> => {
    const result = await extendDevEnvironment(id, { ttl });
    if (result.error || !result.environment) {
      return { error: result.error ?? 'Failed to extend dev environment.' };
    }
    setEnvironments((prev) => prev.map((e) => (e.id === id ? result.environment! : e)));
    return { error: null };
  }, []);

  const wake = useCallback(async (id: string): Promise<{ error: string | null }> => {
    setPendingWakeIds((prev) => new Set(prev).add(id));
    const result = await wakeDevEnvironment(id);
    if (result.error || !result.environment) {
      setPendingWakeIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      return { error: result.error ?? 'Failed to wake dev environment.' };
    }
    setEnvironments((prev) => prev.map((e) => (e.id === id ? result.environment! : e)));
    // Leave id in pendingWakeIds — the environment is very likely still
    // Sleeping/Provisioning in this immediate response; reconcilePendingWakes
    // clears it once a poll actually observes the environment leave Sleeping.
    return { error: null };
  }, []);

  const destroy = useCallback(async (id: string): Promise<{ error: string | null }> => {
    const result = await destroyDevEnvironment(id);
    if (result.error) return { error: result.error };
    setEnvironments((prev) => prev.filter((e) => e.id !== id));
    setPendingWakeIds((prev) => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
    return { error: null };
  }, []);

  const kubeconfig = useCallback(async (id: string) => kubeconfigDevEnvironment(id), []);

  return { environments, limits, error, refresh, create, extend, wake, destroy, kubeconfig };
}
