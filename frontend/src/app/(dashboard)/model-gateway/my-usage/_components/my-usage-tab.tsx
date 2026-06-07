'use client';

import Link from 'next/link';
import { Alert, Button, Icon, NativeSelect, Progress, Stack, Text } from '@orbit/terra';
import type { PAT } from '@orbit/shared/api';
import { fmtCost } from '@orbit/utils';
import { useAnalyticsMeContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';
import type { QuotaStatus } from '../../_types';

interface MyUsageTabProps {
  pats: PAT[];
  initialQuota: QuotaStatus | null;
}

function BudgetBanner({ quota }: { quota: QuotaStatus }) {
  if (!quota.has_limit) return null;

  const pct =
    quota.monthly_budget_usd > 0 ? (quota.current_spend_usd / quota.monthly_budget_usd) * 100 : 0;
  const variant = quota.spend_limit_reached ? 'error' : pct >= 80 ? 'degraded' : 'healthy';

  return (
    <Stack gap="snug">
      {quota.spend_limit_reached && (
        <Alert variant="error">
          Monthly budget reached — inference requests are blocked until the next billing cycle or
          your limit is raised.
        </Alert>
      )}
      <Stack gap="snug">
        <Stack direction="horizontal" align="center" justify="between">
          <Text size="supporting" color="muted">
            Monthly budget
          </Text>
          <Text size="supporting" color="foreground" mono>
            {fmtCost(quota.current_spend_usd)} / {fmtCost(quota.monthly_budget_usd)}
          </Text>
        </Stack>
        <Progress
          value={quota.current_spend_usd}
          max={quota.monthly_budget_usd}
          label="Monthly budget utilisation"
          variant={variant}
          showValue
        />
      </Stack>
    </Stack>
  );
}

export function MyUsageTab({ pats, initialQuota }: MyUsageTabProps) {
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
      {initialQuota && <BudgetBanner quota={initialQuota} />}
      <AnalyticsDashboard
        title="My Usage"
        description="Your personal inference activity — spend, tokens, and request volume."
        data={data}
        status={status}
        error={error}
        days={days}
        granularity={granularity}
        onSetDays={setDays}
        onSetGranularity={setGranularity}
        filterSlot={filterSlot}
      />
    </Stack>
  );
}
