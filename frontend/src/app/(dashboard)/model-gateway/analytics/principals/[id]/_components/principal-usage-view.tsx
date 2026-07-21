'use client';

/** @tier 3 — model-gateway/analytics/principals/[id]-scoped. Admin drill-down view. */

import Link from 'next/link';
import { Button, Icon, Stack } from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing';
import { usePrincipalAnalytics } from '../../../../_hooks/use-principal-analytics';
import { AnalyticsDashboard } from '../../../../_components/analytics/analytics-dashboard';
import type { AnalyticsResponse } from '../../../../_types';

interface PrincipalUsageViewProps {
  principalId: string;
  displayName: string | null;
  email: string | null;
  initialData: AnalyticsResponse | null;
  initialError: string | null;
}

export function PrincipalUsageView({
  principalId,
  displayName,
  email,
  initialData,
  initialError,
}: PrincipalUsageViewProps) {
  const { data, status, error, days, granularity, setDays, setGranularity } = usePrincipalAnalytics(
    principalId,
    initialData,
    initialError,
    undefined,
  );

  const primaryLabel = displayName || email || principalId;
  const secondaryLabel = displayName && email ? email : displayName || email ? null : principalId;

  return (
    <Stack gap="comfortable">
      {/* Breadcrumb */}
      <Stack direction="horizontal">
        <Button variant="ghost" size="sm" asChild>
          <Link href={`${APP_ROUTES.modelGateway.analytics}?tab=principals`}>
            <Icon name="arrow-left" size="xs" />
            Analytics
          </Link>
        </Button>
      </Stack>

      <AnalyticsDashboard
        key={principalId}
        title={`Usage — ${primaryLabel}`}
        description={
          secondaryLabel
            ? `Full inference activity for ${primaryLabel} (${secondaryLabel}) — spend, tokens, and request volume.`
            : `Full inference activity for ${primaryLabel} — spend, tokens, and request volume.`
        }
        data={data}
        status={status}
        error={error}
        days={days}
        granularity={granularity}
        periodPrefKey="analytics.defaultPeriod"
        onSetDays={setDays}
        onSetGranularity={setGranularity}
      />
    </Stack>
  );
}
