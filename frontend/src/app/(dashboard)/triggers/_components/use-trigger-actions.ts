'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`).
 * Stays Tier 3 deliberately: the hook is glue between the drawer and the
 * triggers API, and moves with both.
 *
 * The mutation callbacks behind every trigger surface: fire, toggle, safety,
 * edit, rotate, redeliver, delete. Owned here once so the platform Triggers
 * page and the agent page drive the same drawer with the same behaviour,
 * differing only in how they refresh and where they put a page-level notice.
 */

import { useCallback, useState } from 'react';
import {
  deleteTrigger,
  fireTrigger,
  redeliverDelivery,
  updateSource,
  updateTrigger,
} from '../_services/triggers-api';
import { toTriggerUpdateRequest } from '../_types';
import type { DeliveryRow, TriggerRow } from '../_types';
import type { TriggerEditPatch } from './trigger-drawer';

export interface TriggerNotice {
  ok: boolean;
  message: string;
}

export interface TriggerActionsOptions {
  /** Reloads the lists after a successful mutation. */
  refresh: () => void;
  /** Receives page-level notices: failures, and the queued-fire confirmation. */
  notify: (notice: TriggerNotice) => void;
}

/**
 * Everything a trigger surface can do to a trigger, wired to the shared API
 * client. State that belongs to a single control flow (which trigger is
 * firing, which run form is open, the delete confirmation) lives here too, so
 * a caller mounts the drawer and the modals without re-deriving any of it.
 */
export function useTriggerActions({ refresh, notify }: TriggerActionsOptions) {
  const [firingId, setFiringId] = useState<string | null>(null);
  /** Manual trigger whose input form is open. */
  const [runFormTrigger, setRunFormTrigger] = useState<TriggerRow | null>(null);
  /** Trigger awaiting delete confirmation. */
  const [deleteCandidate, setDeleteCandidate] = useState<TriggerRow | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const fireManualTrigger = useCallback(
    async (row: TriggerRow, variables?: Record<string, string>): Promise<string | null> => {
      setFiringId(row.id);
      const result = await fireTrigger(row.id, variables);
      setFiringId(null);
      if (result.error !== null) {
        notify({ ok: false, message: `${row.name}: ${result.error}` });
        return result.error;
      }
      // The fire is durably queued and the run starts moments later, so there
      // is no run to name yet. Reporting one here would mean starting the run
      // before acknowledging, which is what loses fires on a restart.
      notify({ ok: true, message: `${row.name}: queued. The run starts shortly.` });
      // The delivery exists now; refresh so the Deliveries surfaces show it.
      refresh();
      return null;
    },
    [refresh, notify],
  );

  /**
   * Running a manual trigger goes through its declared form when it has one;
   * a trigger with no inputs fires directly. The same declaration validates
   * the API call, so the form cannot drift from what /fire accepts.
   */
  const startManualRun = useCallback(
    (row: TriggerRow) => {
      if ((row.manualInputs ?? []).length > 0) {
        setRunFormTrigger(row);
        return;
      }
      void fireManualTrigger(row);
    },
    [fireManualTrigger],
  );

  /**
   * Rotates a webhook trigger's source secret; the drawer shows it once. A
   * provider that issues the secret takes `signingSecret` and returns none, so
   * success resolves with an empty string. `firstSecret` sets the first secret
   * of a source that is waiting for one, which needs no rotation.
   */
  const rotateSecret = useCallback(
    async (
      row: TriggerRow,
      signingSecret?: string,
      firstSecret = false,
    ): Promise<string | null> => {
      const result = await updateSource(row.sourceId, {
        ...(!firstSecret && { rotate_secret: true }),
        ...(signingSecret !== undefined && { signing_secret: signingSecret }),
      });
      if (result.error || (signingSecret === undefined && !result.source?.secret)) {
        notify({
          ok: false,
          message: `${row.name}: ${result.error ?? 'rotation returned no secret'}`,
        });
        return null;
      }
      // Without this the row keeps signingSecretSet=false, so the drawer keeps
      // waiting and a second save would omit rotate_secret and be rejected.
      refresh();
      return result.source?.secret ?? '';
    },
    [refresh, notify],
  );

  const toggleTrigger = useCallback(
    async (row: TriggerRow) => {
      const result = await updateTrigger(row.id, toTriggerUpdateRequest({ enabled: !row.enabled }));
      if (result.error) {
        notify({ ok: false, message: `${row.name}: ${result.error}` });
        return;
      }
      refresh();
    },
    [refresh, notify],
  );

  /**
   * Persists a safety setting from the drawer.
   *
   * The drawer stays open: these are settings a reader adjusts while looking at
   * the trigger, not an action that finishes with it, so closing the panel
   * underneath them would lose their place.
   */
  const updateSafety = useCallback(
    async (row: TriggerRow, patch: Partial<TriggerRow>) => {
      const result = await updateTrigger(row.id, toTriggerUpdateRequest(patch));
      if (result.error) {
        notify({ ok: false, message: `${row.name}: ${result.error}` });
        return;
      }
      refresh();
    },
    [refresh, notify],
  );

  /**
   * Persists a correction made in the drawer.
   *
   * Unlike the safety controls, this resolves with the failure rather than
   * raising a page-level notice: the drawer keeps the edit on screen next to
   * the reason, so nothing typed is lost to a rejected save.
   */
  const saveTriggerEdit = useCallback(
    async (row: TriggerRow, patch: TriggerEditPatch): Promise<string | null> => {
      const result = await updateTrigger(row.id, toTriggerUpdateRequest(patch));
      if (result.error) return result.error;
      refresh();
      return null;
    },
    [refresh],
  );

  /**
   * Replays a delivery.
   *
   * The refresh follows the call so the new arrival shows up in the list; the
   * result is handed back to the caller, which reports it beside the delivery
   * rather than as a page-level notice.
   */
  const redeliver = useCallback(
    async (row: DeliveryRow): Promise<{ deliveryId: string | null; error: string | null }> => {
      const result = await redeliverDelivery(row.id);
      if (result.error === null) refresh();
      return result;
    },
    [refresh],
  );

  const confirmDelete = useCallback(async () => {
    if (!deleteCandidate) return;
    setDeleting(true);
    setDeleteError(null);
    const result = await deleteTrigger(deleteCandidate.id);
    setDeleting(false);
    if (result.error) {
      setDeleteError(result.error);
      return;
    }
    setDeleteCandidate(null);
    refresh();
  }, [deleteCandidate, refresh]);

  const closeDeleteDialog = useCallback(() => {
    setDeleteCandidate(null);
    setDeleteError(null);
  }, []);

  return {
    fireManualTrigger,
    startManualRun,
    rotateSecret,
    toggleTrigger,
    updateSafety,
    saveTriggerEdit,
    redeliver,
    firingId,
    runFormTrigger,
    setRunFormTrigger,
    deleteCandidate,
    setDeleteCandidate,
    deleting,
    deleteError,
    confirmDelete,
    closeDeleteDialog,
  };
}
