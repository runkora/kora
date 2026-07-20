'use client';

import Link from 'next/link';
import { Alert, Button, Icon, NativeSelect, Progress, Stack, Text } from '@orbit/terra';
import type { PAT } from '@orbit/shared/api';
import { fmtCost } from '@orbit/utils';
import { useAnalyticsMeContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';
import type { BudgetCeilingStatus, BudgetStatus } from '../../_types';

interface MyUsageTabProps {
  pats: PAT[];
  initialBudget: BudgetStatus | null;
}

// ceilingLabel names one spending ceiling for the banner.
function ceilingLabel(c: BudgetCeilingStatus): string {
  switch (c.scope) {
    case 'role_pool':
      return `Team pool (${c.id}), ${c.window}`;
    case 'tenant':
      return `Tenant cap, ${c.window}`;
    case 'run':
      return 'Current run';
    default:
      return c.window === 'weekly' ? 'Weekly budget' : 'Monthly budget';
  }
}

function CeilingBar({ ceiling }: { ceiling: BudgetCeilingStatus }) {
  const pct = ceiling.limit_usd > 0 ? (ceiling.spend_usd / ceiling.limit_usd) * 100 : 0;
  const variant = ceiling.reached ? 'error' : pct >= 80 ? 'degraded' : 'healthy';
  const label = ceilingLabel(ceiling);

  return (
    <Stack gap="snug">
      <Stack direction="horizontal" align="center" justify="between">
        <Text size="supporting" color="muted">
          {label}
        </Text>
        <Text size="supporting" color="foreground" mono>
          {fmtCost(ceiling.spend_usd)} / {fmtCost(ceiling.limit_usd)}
        </Text>
      </Stack>
      <Progress
        value={ceiling.spend_usd}
        max={ceiling.limit_usd}
        label={`${label} utilisation`}
        variant={variant}
        showValue
      />
    </Stack>
  );
}

function BudgetBanner({ budget }: { budget: BudgetStatus }) {
  if (!budget.has_limit) return null;

  // One bar per applicable ceiling; fall back to the legacy monthly fields
  // when the ceilings list is absent.
  const ceilings: BudgetCeilingStatus[] =
    budget.ceilings && budget.ceilings.length > 0
      ? budget.ceilings
      : [
          {
            scope: 'principal',
            id: '',
            window: 'monthly',
            limit_usd: budget.monthly_budget_usd,
            spend_usd: budget.current_spend_usd,
            reached: budget.spend_limit_reached,
          },
        ];

  return (
    <Stack gap="snug">
      {budget.spend_limit_reached && (
        <Alert variant="error">
          A budget ceiling has been reached. Inference requests are blocked until the period resets
          or the limit is raised.
        </Alert>
      )}
      {ceilings.map((c) => (
        <CeilingBar key={`${c.scope}:${c.id}:${c.window}`} ceiling={c} />
      ))}
    </Stack>
  );
}

export function MyUsageTab({ pats, initialBudget }: MyUsageTabProps) {
  const { data, status, error, days, granularity, tokenId, setDays, setGranularity, setTokenId } =
    useAnalyticsMeContext();

  const filterSlot =
    pats.length > 0 ? (
      <Stack minWidth="form-field-sm" maxWidth="form-field-sm">
        <NativeSelect
          value={tokenId ?? ''}
          onChange={(e) => setTokenId(e.target.value || null)}
          aria-label="Filter by API key"
        >
          <option value="">All API keys</option>
          {pats.map((pat) => (
            <option key={pat.id} value={pat.id}>
              {pat.name}
            </option>
          ))}
        </NativeSelect>
      </Stack>
    ) : (
      <Stack direction="horizontal" align="center" gap="snug">
        <Icon name="key" size="2xs" color="subtle" />
        <Button asChild variant="ghost" size="sm">
          <Link href="/model-gateway/keys">No API keys</Link>
        </Button>
      </Stack>
    );

  return (
    <Stack gap="comfortable">
      {initialBudget && <BudgetBanner budget={initialBudget} />}
      <AnalyticsDashboard
        title="My Usage"
        description="Your personal inference activity: spend, tokens, and request volume."
        data={data}
        status={status}
        error={error}
        days={days}
        granularity={granularity}
        periodPrefKey="myUsage.defaultPeriod"
        onSetDays={setDays}
        onSetGranularity={setGranularity}
        filterSlot={filterSlot}
      />
    </Stack>
  );
}
