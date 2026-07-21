/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useCallback, useState } from 'react';
import {
  Button,
  ButtonGroup,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Text,
} from '@orbit/terra';
import type { BudgetAssignment } from '../../../policy/_types/api';

interface BudgetEditModalProps {
  open: boolean;
  /** "role" budgets carry a pooled toggle; "grant" budgets carry a per-run limit. */
  kind: 'role' | 'grant';
  /** Display name of the role or grant principal being edited. */
  subject: string;
  initialBudget: BudgetAssignment | null | undefined;
  onClose: () => void;
  /** Persist the new budget; null removes the assignment. */
  onSave: (
    budget: BudgetAssignment | null,
  ) => Promise<{ ok: true } | { ok: false; message: string }>;
}

export function BudgetEditModal({
  open,
  kind,
  subject,
  initialBudget,
  onClose,
  onSave,
}: BudgetEditModalProps) {
  const [limitUSD, setLimitUSD] = useState(
    initialBudget?.limit_usd ? String(initialBudget.limit_usd) : '',
  );
  const [cadence, setCadence] = useState(initialBudget?.reset_cadence ?? 'monthly');
  const [pooled, setPooled] = useState(initialBudget?.pooled ?? false);
  const [perRunUSD, setPerRunUSD] = useState(
    initialBudget?.per_run_limit_usd ? String(initialBudget.per_run_limit_usd) : '',
  );
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const parsedLimit = limitUSD === '' ? 0 : parseFloat(limitUSD);
  const parsedPerRun = perRunUSD === '' ? 0 : parseFloat(perRunUSD);
  const limitValid = !Number.isNaN(parsedLimit) && parsedLimit >= 0;
  const perRunValid = !Number.isNaN(parsedPerRun) && parsedPerRun >= 0;
  // Roles require a window limit; grants require at least one of the two limits.
  const valid =
    limitValid &&
    perRunValid &&
    (kind === 'role' ? parsedLimit > 0 : parsedLimit > 0 || parsedPerRun > 0);

  const submit = useCallback(
    async (budget: BudgetAssignment | null) => {
      setSubmitting(true);
      setSubmitError(null);
      const result = await onSave(budget);
      if (!result.ok) {
        setSubmitError(result.message);
      }
      setSubmitting(false);
    },
    [onSave],
  );

  const buildBudget = (): BudgetAssignment => {
    const budget: BudgetAssignment = { reset_cadence: cadence };
    if (parsedLimit > 0) budget.limit_usd = parsedLimit;
    if (kind === 'role' && pooled) budget.pooled = true;
    if (kind === 'grant' && parsedPerRun > 0) budget.per_run_limit_usd = parsedPerRun;
    return budget;
  };

  return (
    <Modal open={open} onClose={onClose} size="md">
      <ModalHeader title={`${initialBudget ? 'Edit' : 'Set'} Budget: ${subject}`} />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              {kind === 'role' ? 'Limit' : 'Window Limit'}
            </Text>
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption" color="muted">
                $
              </Text>
              <Input
                type="number"
                placeholder="e.g. 50"
                value={limitUSD}
                onChange={(e) => setLimitUSD(e.target.value)}
              />
              <ButtonGroup role="group" aria-label="Reset window">
                <Button
                  variant="outline-muted"
                  size="sm"
                  selected={cadence === 'weekly'}
                  onClick={() => setCadence('weekly')}
                >
                  Weekly
                </Button>
                <Button
                  variant="outline-muted"
                  size="sm"
                  selected={cadence === 'monthly'}
                  onClick={() => setCadence('monthly')}
                >
                  Monthly
                </Button>
              </ButtonGroup>
            </Stack>
          </Stack>

          {kind === 'role' && (
            <Stack gap="tight">
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="caption" weight="emphasis">
                  Shared Pool
                </Text>
                <Button
                  variant={pooled ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setPooled((v) => !v)}
                >
                  {pooled ? 'Pooled' : 'Per member'}
                </Button>
              </Stack>
              <Text size="caption" color="subtle">
                Per member: every principal asserting the role gets this limit. Pooled: one shared
                pot where all members&apos; spend accumulates against a single counter.
              </Text>
            </Stack>
          )}

          {kind === 'grant' && (
            <Stack gap="tight">
              <Text size="caption" weight="emphasis">
                Per-Run Limit
              </Text>
              <Stack direction="horizontal" align="center" gap="snug">
                <Text size="caption" color="muted">
                  $
                </Text>
                <Input
                  type="number"
                  placeholder="e.g. 5"
                  value={perRunUSD}
                  onChange={(e) => setPerRunUSD(e.target.value)}
                />
                <Text size="caption" color="muted">
                  / run
                </Text>
              </Stack>
              <Text size="caption" color="subtle">
                Maximum spend in a single agent run; every new run starts fresh. Set either limit or
                both.
              </Text>
            </Stack>
          )}

          <Text size="caption" color="subtle">
            Requests are blocked once spend reaches any applicable ceiling. Weekly windows reset
            Monday 00:00 UTC; monthly windows reset on the 1st.
          </Text>

          {submitError && (
            <Text color="status-error" size="caption">
              {submitError}
            </Text>
          )}
        </Stack>
      </ModalBody>
      <ModalFooter>
        {initialBudget && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => void submit(null)}
            disabled={submitting}
          >
            Remove Budget
          </Button>
        )}
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          size="sm"
          onClick={() => void submit(buildBudget())}
          disabled={submitting || !valid}
        >
          {submitting ? 'Saving...' : 'Save Budget'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
