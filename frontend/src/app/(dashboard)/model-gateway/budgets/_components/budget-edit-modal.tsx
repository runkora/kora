/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useCallback, useState } from 'react';
import {
  Button,
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
  /** Display name of the role or grant principal being edited. */
  subject: string;
  initialBudget: BudgetAssignment | null | undefined;
  onClose: () => void;
  /** Persist the new budget; null removes the assignment. Throws message on failure. */
  onSave: (
    budget: BudgetAssignment | null,
  ) => Promise<{ ok: true } | { ok: false; message: string }>;
}

export function BudgetEditModal({
  open,
  subject,
  initialBudget,
  onClose,
  onSave,
}: BudgetEditModalProps) {
  const [monthlyLimitUSD, setMonthlyLimitUSD] = useState(
    initialBudget ? String(initialBudget.monthly_limit_usd) : '',
  );
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const parsed = parseFloat(monthlyLimitUSD);
  const valid = !Number.isNaN(parsed) && parsed > 0;

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

  return (
    <Modal open={open} onClose={onClose} size="md">
      <ModalHeader title={`${initialBudget ? 'Edit' : 'Set'} Budget — ${subject}`} />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              Monthly Limit
            </Text>
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption" color="muted">
                $
              </Text>
              <Input
                type="number"
                placeholder="e.g. 50"
                value={monthlyLimitUSD}
                onChange={(e) => setMonthlyLimitUSD(e.target.value)}
              />
              <Text size="caption" color="muted">
                / month
              </Text>
            </Stack>
            <Text size="caption" color="subtle">
              Inference requests are blocked once spend reaches the highest limit across all
              applicable budgets. Resets at the start of each calendar month.
            </Text>
          </Stack>

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
          onClick={() => void submit({ monthly_limit_usd: parsed, reset_cadence: 'monthly' })}
          disabled={submitting || !valid}
        >
          {submitting ? 'Saving...' : 'Save Budget'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
