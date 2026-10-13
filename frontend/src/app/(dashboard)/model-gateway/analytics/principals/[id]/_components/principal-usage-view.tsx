'use client';

/** @tier 3 — model-gateway/analytics/principals/[id]-scoped. Admin drill-down view. */

import Link from 'next/link';
import { Button, Icon, Stack, EmptyState } from '@runkora/hades';
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
  // True when principalId resolved to neither a known identity nor any
  // inference_usage activity — see the notFound heuristic in page.tsx.
  // Renders a dedicated not-found state instead of an all-zero dashboard, so
  // a bogus/guessed URL can't produce something that looks like a real
  // (if quiet) usage report.
  notFound: boolean;
}

function BackToAnalyticsLink() {
  return (
    <Stack direction="horizontal">
      <Button variant="ghost" size="sm" asChild>
        <Link href={`${APP_ROUTES.modelGateway.analytics}?tab=principals`}>
          <Icon name="arrow-left" size="xs" />
          Analytics
        </Link>
      </Button>
    </Stack>
  );
}

export function PrincipalUsageView({
  principalId,
  displayName,
  email,
  initialData,
  initialError,
  notFound,
}: PrincipalUsageViewProps) {
  const { data, status, error, days, granularity, setDays, setGranularity } = usePrincipalAnalytics(
    principalId,
    initialData,
    initialError,
    undefined,
  );

  if (notFound) {
    return (
      <Stack gap="comfortable">
        <BackToAnalyticsLink />
        <EmptyState
          icon="search-x"
          title="Principal not found"
          description={`No user or usage activity was found for "${principalId}". It may not exist, or may belong to a different tenant.`}
          action={
            <Button asChild>
              <Link href={`${APP_ROUTES.modelGateway.analytics}?tab=principals`}>
                Back to Analytics
              </Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  const primaryLabel = displayName || email || principalId;
  const secondaryLabel = displayName && email ? email : displayName || email ? null : principalId;

  return (
    <Stack gap="comfortable">
      <BackToAnalyticsLink />

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
