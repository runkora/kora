'use client';

/** @tier 3 — agents/[slug]-scoped. Budget usage card for the agent overview tab. */

import { Alert, Card, CardContent, CardHeader, Progress, Stack, Text } from '@orbit/terra';
import { fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../../model-gateway/_types';

interface AgentBudgetCardProps {
  /** The agent's row from the budgets listing; null when unavailable. */
  budget: BudgetPrincipalStatus | null;
  /** Tenant cap status — agents are blocked when it is reached. */
  tenantCap: TenantCapStatus | null;
}

export function AgentBudgetCard({ budget, tenantCap }: AgentBudgetCardProps) {
  const windowCategories = budget?.categories.filter((c) => c.limit_usd > 0) ?? [];
  const perRunLimits = budget?.categories.filter((c) => (c.per_run_limit_usd ?? 0) > 0) ?? [];
  const hasAnything =
    windowCategories.length > 0 || perRunLimits.length > 0 || (tenantCap?.reached ?? false);

  return (
    <Card>
      <CardHeader compact title="Budget" divider />
      <CardContent padded>
        {hasAnything ? (
          <Stack gap="snug">
            {tenantCap?.reached && (
              <Alert variant="error">
                Tenant cap reached — this agent&apos;s inference requests are blocked until the
                period resets or the cap is raised.
              </Alert>
            )}
            {budget?.spend_limit_reached && (
              <Alert variant="error">
                Budget ceiling reached — this agent&apos;s inference requests are blocked until the
                period resets or its limit is raised.
              </Alert>
            )}

            {windowCategories.map((cat) => {
              // cat.spend_usd is the principal's canonical window spend — the
              // same number enforcement evaluates (weekly and monthly alike).
              const spend = cat.spend_usd;
              const pct = cat.limit_usd > 0 ? (spend / cat.limit_usd) * 100 : 0;
              const variant = spend >= cat.limit_usd ? 'error' : pct >= 80 ? 'degraded' : 'healthy';
              return (
                <Stack key={`${cat.source}:${cat.id}`} gap="tight">
                  <Stack direction="horizontal" align="center" justify="between">
                    <Text size="supporting" color="muted">
                      {cat.reset_cadence === 'weekly' ? 'Weekly' : 'Monthly'} budget
                    </Text>
                    <Text size="supporting" mono>
                      {fmtUsd(spend)} / {fmtUsd(cat.limit_usd)}
                      {fmtWindowSuffix(cat.reset_cadence)}
                    </Text>
                  </Stack>
                  <Progress
                    value={spend}
                    max={cat.limit_usd}
                    label={`${cat.reset_cadence} budget utilisation`}
                    variant={variant}
                    showValue
                  />
                </Stack>
              );
            })}

            {perRunLimits.map((cat) => (
              <Stack
                key={`run:${cat.source}:${cat.id}`}
                direction="horizontal"
                align="center"
                justify="between"
              >
                <Text size="supporting" color="muted">
                  Per-run limit
                </Text>
                <Text size="supporting" mono>
                  {fmtUsd(cat.per_run_limit_usd)} / run
                </Text>
              </Stack>
            ))}
          </Stack>
        ) : (
          <Text size="supporting" color="muted">
            No budget configured — set one in Settings. Spend still counts toward the tenant cap.
          </Text>
        )}
      </CardContent>
    </Card>
  );
}
