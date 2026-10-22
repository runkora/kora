/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useCallback, useState } from 'react';
import { Button, Modal, ModalBody, ModalFooter, ModalHeader, Stack, Text } from '@runkora/hades';
import {
  BudgetLimitFields,
  budgetFromLimitValues,
  budgetLimitProblem,
  budgetLimitValuesFrom,
} from '@runkora/components';
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
  const [limits, setLimits] = useState(() => budgetLimitValuesFrom(initialBudget));
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const problem = budgetLimitProblem(limits, kind);
  const valid = problem === null;

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

  // Saving always keeps the other row's value, so editing one limit never drops the other.
  const buildBudget = (): BudgetAssignment => budgetFromLimitValues(limits, kind)!;

  return (
    <Modal open={open} onClose={onClose} size="md">
      <ModalHeader title={`${initialBudget ? 'Edit' : 'Set'} Budget: ${subject}`} />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <BudgetLimitFields
            mode={kind}
            values={limits}
            onChange={(patch) => setLimits((v) => ({ ...v, ...patch }))}
          />

          <Text size="caption" color="subtle">
            Requests are blocked once spend reaches any applicable ceiling. Weekly windows reset
            Monday 00:00 UTC; monthly windows reset on the 1st.
          </Text>

          {problem && (
            <Text size="caption" color="muted">
              {problem}
            </Text>
          )}

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
