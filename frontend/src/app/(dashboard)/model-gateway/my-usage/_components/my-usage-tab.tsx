'use client';

import Link from 'next/link';
import { Button, Icon, NativeSelect, Stack } from '@orbit/terra';
import type { PAT } from '@orbit/shared/api';
import { useAnalyticsMeContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';

interface MyUsageTabProps {
  pats: PAT[];
}

export function MyUsageTab({ pats }: MyUsageTabProps) {
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
  );
}
