'use client';

// ── useDevEnvironments ────────────────────────────────────────────────────────
//
// Owns all reactive state for the /dev-environments page: the list, and every
// lifecycle action a row can trigger. Polls the list while any environment is
// still Pending/Provisioning — mirrors the agent-type detail page's own
// provisioning-poll pattern (agents/[slug]/_hooks/use-agent-detail.ts).

import { useCallback, useEffect, useRef, useState } from 'react';
import type { DevEnvironment, DevEnvironmentKubeconfigResponse } from '@orbit/shared/api';
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

export function useDevEnvironments(
  initial: DevEnvironment[],
  initialError: string | null,
): UseDevEnvironmentsShape {
  const [environments, setEnvironments] = useState<DevEnvironment[]>(initial);
  const [error, setError] = useState<string | null>(initialError);

  const refresh = useCallback(async () => {
    const result = await fetchDevEnvironments();
    if (result.error) {
      setError(result.error);
      return;
    }
    setError(null);
    setEnvironments(result.data);
  }, []);

  // Poll while any environment is still being provisioned.
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const hasInFlight = environments.some((e) => isInFlight(e.phase));
  useEffect(() => {
    if (!hasInFlight) return;
    pollingRef.current = setInterval(() => {
      void fetchDevEnvironments().then((result) => {
        if (!result.error) setEnvironments(result.data);
      });
    }, POLL_INTERVAL_MS);
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [hasInFlight]);

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
    const result = await wakeDevEnvironment(id);
    if (result.error || !result.environment) {
      return { error: result.error ?? 'Failed to wake dev environment.' };
    }
    setEnvironments((prev) => prev.map((e) => (e.id === id ? result.environment! : e)));
    return { error: null };
  }, []);

  const destroy = useCallback(async (id: string): Promise<{ error: string | null }> => {
    const result = await destroyDevEnvironment(id);
    if (result.error) return { error: result.error };
    setEnvironments((prev) => prev.filter((e) => e.id !== id));
    return { error: null };
  }, []);

  const kubeconfig = useCallback(async (id: string) => kubeconfigDevEnvironment(id), []);

  return { environments, error, refresh, create, extend, wake, destroy, kubeconfig };
}
