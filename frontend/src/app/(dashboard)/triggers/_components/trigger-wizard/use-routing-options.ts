'use client';

/**
 * @tier 3 — triggers-scoped. Loads what "Let kora choose" offers: the agents a
 * routed trigger may name, each marked runnable for the caller, and the
 * decision models in the tenant's catalog.
 */

import { useEffect, useState } from 'react';
import { listModels, listRoutingCandidates } from '../../_services/triggers-api';
import { decisionModelsOf, toRoutingAgent } from './wizard-state';
import type { RoutingOptions } from './types';

const LOADING: RoutingOptions = {
  candidates: { status: 'loading' },
  models: { status: 'loading' },
};

/** Undefined while `enabled` is false, so the Target step offers one agent only. */
export function useRoutingOptions(enabled: boolean): RoutingOptions | undefined {
  const [options, setOptions] = useState<RoutingOptions>(LOADING);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    void Promise.all([listRoutingCandidates(), listModels()]).then(([candidates, models]) => {
      if (!live) return;
      setOptions({
        candidates:
          candidates.error !== null
            ? { status: 'error', message: candidates.error }
            : { status: 'ready', items: candidates.candidates.map(toRoutingAgent) },
        models:
          models.error !== null
            ? { status: 'error', message: models.error }
            : { status: 'ready', items: decisionModelsOf(models.models) },
      });
    });
    return () => {
      live = false;
    };
  }, [enabled]);
  return enabled ? options : undefined;
}
