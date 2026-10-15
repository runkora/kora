'use client';

// ── useSkillDetail hook ───────────────────────────────────────────────────────
//
// Owns the Skill that the user opened from the overview. The selection stays
// in client state, so the address stays `/marketplace` and no history entry is
// added. Each open goes through the BFF:
//
//   1. Abort the request in flight and start a new request.
//   2. Ignore a response that is not from the newest request.
//   3. On success, show the Skill. On a 404, show "not found". On any other
//      failure, show an error with a retry.
//
// Closing the Skill aborts its request. The browse state lives in
// useMarketplace, so the overview shows the same results again.

import { useCallback, useEffect, useRef, useState } from 'react';
import type { Skill } from '@orbit/shared/api';
import { fetchSkillDetail } from '../_services/marketplace-api';

export type SkillDetailStatus = 'loading' | 'success' | 'not_found' | 'error';

/** The open Skill and its actions. Components use it through useMarketplaceContext(). */
export interface SkillDetailShape {
  /** The skillSlug of the open Skill, or null when the overview shows the results. */
  openSlug: string | null;
  /** The loaded Skill. Null until the request for openSlug succeeds. */
  skill: Skill | null;
  /** The status of the open Skill. Meaningless when openSlug is null. */
  skillStatus: SkillDetailStatus;
  skillError: string | null;
  /** Opens a Skill of a Marketplace in place of the results. */
  openSkill(marketplaceSlug: string, skillSlug: string): void;
  /** Closes the Skill and shows the results again. */
  closeSkill(): void;
  /** Sends the failed Skill request again. */
  retrySkill(): void;
}

/** The two values that identify a Skill in the skills-service. */
interface SkillTarget {
  marketplaceSlug: string;
  skillSlug: string;
}

export function useSkillDetail(): SkillDetailShape {
  // The open Skill: the skills-service needs its Marketplace and its slug.
  const [openTarget, setOpenTarget] = useState<SkillTarget | null>(null);
  const [skill, setSkill] = useState<Skill | null>(null);
  const [skillStatus, setSkillStatus] = useState<SkillDetailStatus>('loading');
  const [skillError, setSkillError] = useState<string | null>(null);

  // The controller of the request in flight. A newer request aborts it.
  const controllerRef = useRef<AbortController | null>(null);
  // Increases by 1 for each request and each close. An older response is ignored.
  const requestIdRef = useRef(0);

  const abortInFlight = useCallback(() => {
    if (controllerRef.current) {
      controllerRef.current.abort();
      controllerRef.current = null;
    }
    requestIdRef.current += 1;
  }, []);

  const load = useCallback(
    async (target: SkillTarget) => {
      // 1. Abort the request in flight and start a new request.
      abortInFlight();
      const controller = new AbortController();
      controllerRef.current = controller;
      const requestId = requestIdRef.current;

      setOpenTarget(target);
      setSkill(null);
      setSkillError(null);
      setSkillStatus('loading');

      const result = await fetchSkillDetail(
        target.marketplaceSlug,
        target.skillSlug,
        controller.signal,
      );

      // 2. Ignore a response that is not from the newest request.
      if (result.cancelled || requestId !== requestIdRef.current) {
        return;
      }
      controllerRef.current = null;

      // 3. Show the result.
      if (result.skill) {
        setSkill(result.skill);
        setSkillStatus('success');
      } else if (result.notFound) {
        setSkillStatus('not_found');
      } else {
        setSkillError(result.error);
        setSkillStatus('error');
      }
    },
    [abortInFlight],
  );

  // On unmount, stop the request in flight.
  useEffect(() => abortInFlight, [abortInFlight]);

  const openSkill = useCallback(
    (marketplaceSlug: string, skillSlug: string) => {
      void load({ marketplaceSlug, skillSlug });
    },
    [load],
  );

  const closeSkill = useCallback(() => {
    abortInFlight();
    setOpenTarget(null);
    setSkill(null);
    setSkillError(null);
  }, [abortInFlight]);

  const retrySkill = useCallback(() => {
    if (openTarget !== null) {
      void load(openTarget);
    }
  }, [load, openTarget]);

  return {
    openSlug: openTarget?.skillSlug ?? null,
    skill,
    skillStatus,
    skillError,
    openSkill,
    closeSkill,
    retrySkill,
  };
}
