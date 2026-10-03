'use client';

/** @tier 3 · agents/[slug]-scoped. Budget usage card for the agent overview tab. */

import { Alert, Card, CardContent, CardHeader, Progress, Stack, Text } from '@orbit/terra';
import { fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import { budgetTone, usedShare } from '../../_lib/spend';
import type { BudgetRow } from '../../_lib/spend';

interface AgentBudgetCardProps {
  /** The agent's row from the agent insights; null when it has no limit or insights are unavailable. */
  budget: BudgetRow | null;
}

export function AgentBudgetCard({ budget }: AgentBudgetCardProps) {
  const windows = budget?.windows ?? [];
  const perRunLimit = budget?.perRunLimit ?? 0;
  const reached = windows.some((w) => usedShare(w.spent, w.limit) >= 1);

  return (
    <Card>
      <CardHeader compact title="Budget" divider />
      <CardContent padded>
        {windows.length > 0 || perRunLimit > 0 ? (
          <Stack gap="snug">
            {reached && (
              <Alert variant="error">
                Budget ceiling reached. This agent&apos;s inference requests are blocked until the
                period resets or its limit is raised.
              </Alert>
            )}

            {windows.map((w) => {
              const used = usedShare(w.spent, w.limit) * 100;
              return (
                <Stack key={w.cadence} gap="tight">
                  <Stack direction="horizontal" align="center" justify="between">
                    <Text size="supporting" color="muted">
                      {w.cadence === 'weekly' ? 'Weekly' : 'Monthly'} budget
                    </Text>
                    <Text size="supporting" mono>
                      {fmtUsd(w.spent)} / {fmtUsd(w.limit)}
                      {fmtWindowSuffix(w.cadence)}
                    </Text>
                  </Stack>
                  <Progress
                    value={w.spent}
                    max={w.limit}
                    label={`${w.cadence} budget utilisation`}
                    variant={budgetTone(used)}
                    showValue
                  />
                </Stack>
              );
            })}

            {perRunLimit > 0 && (
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="supporting" color="muted">
                  Per-run limit
                </Text>
                <Text size="supporting" mono>
                  {fmtUsd(perRunLimit)} / run
                </Text>
              </Stack>
            )}
          </Stack>
        ) : (
          <Text size="supporting" color="muted">
            No budget configured. Set one in Settings.
          </Text>
        )}
      </CardContent>
    </Card>
  );
}
