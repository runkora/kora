'use client';

/**
 * @tier 3 — run-chat-scoped. Inline banner shown when a run halts because its
 * budget was exhausted (a 429 BUDGET_EXCEEDED on the agent's LLM calls).
 * Distinct from a failure — the run did nothing wrong, its budget ended.
 * Renders nothing for any other run state.
 */

import { Stack, Text } from '@runkora/hades';
import { fmtUsd } from '@orbit/utils';
import { useRunContext } from '../_context/run-context';

export function BudgetBanner() {
  const { runState, spentUsd, perRunLimitUsd } = useRunContext();

  if (runState !== 'budget_exhausted') {
    return null;
  }

  const detail =
    perRunLimitUsd !== null
      ? `This run spent its ${fmtUsd(perRunLimitUsd)} per-run budget (${fmtUsd(spentUsd)} used).`
      : 'This run reached its budget limit.';

  return (
    <Stack
      direction="horizontal"
      align="center"
      style={{
        gap: 12,
        padding: '6px 14px',
        flexShrink: 0,
        background: 'color-mix(in oklch, var(--chat-error) 18%, transparent)',
        borderBottom: '1px solid var(--chat-bds)',
        fontSize: 12,
        color: 'var(--chat-fg)',
      }}
      role="status"
      aria-live="polite"
    >
      <Text as="span" style={{ fontSize: 12, color: 'var(--chat-fg)' }}>
        {detail} Raise the agent&apos;s per-run limit or budget to start a new run.
      </Text>
    </Stack>
  );
}
