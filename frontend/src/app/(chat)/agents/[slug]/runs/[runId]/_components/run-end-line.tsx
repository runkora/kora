'use client';

/**
 * @tier 3 — run-page-scoped. The line an ended run closes with in place of the composer: how it
 * ended, and for a spent budget whose it was, when it resets and a way to raise it.
 */

import Link from 'next/link';
import { Button, Icon, Stack, Text } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useRunLayout } from '../_lib/run-layout';
import { formatSpan, tokensLine, type RunView } from '../_lib/run-view';
import { LINE } from '../_lib/run-styles';
import { stateLabel } from './run-header';

export function EndLine({ run, canRaiseBudget }: { run: RunView; canRaiseBudget: boolean }) {
  const phone = useRunLayout() === 'phone';
  const b = run.budgetStop;
  const pad = phone ? '12px 16px 16px' : '14px 32px 20px';
  if (run.state === 'budget_exhausted') {
    return (
      <Stack
        direction="horizontal"
        align="center"
        gap="cozy"
        wrap
        style={{ padding: pad, borderTop: LINE }}
      >
        <Icon name="circle-alert" size="2xs" color="status-error" label="" />
        <Stack grow minWidth="none" gap="none">
          <Text size="supporting" weight="emphasis">
            {b
              ? `Stopped: ${b.whose} of $${b.limitUsd.toFixed(2)} is spent`
              : 'Stopped: the budget is spent'}
          </Text>
          <Text size="caption" color="subtle">
            Model calls were refused after ${run.spend.usd.toFixed(2)}.
            {b?.resets ? ` The budget resets ${b.resets}.` : ''}
            {run.record ? ` ${tokensLine(run)}.` : ''}
          </Text>
        </Stack>
        {canRaiseBudget && (
          <Button size="sm" variant="outline" asChild>
            <Link href={APP_ROUTES.modelGateway.budgets}>Raise budget</Link>
          </Button>
        )}
      </Stack>
    );
  }
  const status = stateLabel(run);
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="snug"
      wrap
      style={{ padding: pad, borderTop: LINE }}
    >
      <Icon
        name={status.icon}
        size="2xs"
        color={status.tone === 'status-error' ? 'status-error' : 'accent'}
        label=""
      />
      <Text size="supporting" weight="emphasis">
        {status.label}
      </Text>
      <Text size="supporting" color="subtle">
        in {formatSpan(run.durationMs)} · ${run.spend.usd.toFixed(2)}
        {run.record ? ` · ${tokensLine(run)}` : ''}
      </Text>
    </Stack>
  );
}
