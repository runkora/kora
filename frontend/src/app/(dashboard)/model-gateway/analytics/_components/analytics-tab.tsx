'use client';

import { useCallback, useState } from 'react';
import { NativeSelect, Stack, Tabs, TabsContent, TabsList, TabsTrigger } from '@clear-route/hades';
import type { ModelRow } from '../../_types';
import { useAnalyticsContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';
import { AnalyticsPrincipalsTable } from '../../_components/analytics/analytics-principals-table';

interface AnalyticsTabProps {
  models: ModelRow[];
  /** Deep-linked tab, e.g. `?tab=principals` from the principal drill-down's back link. */
  initialTab?: 'overview' | 'principals';
}

export function AnalyticsTab({ models, initialTab = 'overview' }: AnalyticsTabProps) {
  const { data, status, error, days, granularity, modelId, setDays, setGranularity, setModelId } =
    useAnalyticsContext();

  // Controlled so a deep-link (e.g. the principal drill-down's "Analytics"
  // back link with ?tab=principals) can land on a specific tab, and so
  // switching tabs keeps the URL shareable/refreshable via replaceState.
  const [activeTab, setActiveTab] = useState(initialTab);

  const handleTabChange = useCallback((value: string) => {
    setActiveTab(value as 'overview' | 'principals');
    const qs = value === 'principals' ? '?tab=principals' : '';
    window.history.replaceState(null, '', `${window.location.pathname}${qs}`);
  }, []);

  const filterSlot =
    models.length > 0 ? (
      <Stack minWidth="form-field" maxWidth="form-field">
        <NativeSelect
          value={modelId ?? ''}
          onChange={(e) => setModelId(e.target.value || null)}
          aria-label="Filter by model"
        >
          <option value="">All models</option>
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </NativeSelect>
      </Stack>
    ) : undefined;

  return (
    <Tabs value={activeTab} onValueChange={handleTabChange}>
      <TabsList>
        <TabsTrigger value="overview">Overview</TabsTrigger>
        <TabsTrigger value="principals">Principals</TabsTrigger>
      </TabsList>

      <TabsContent value="overview">
        <AnalyticsDashboard
          title="Analytics"
          description="Tenant-wide inference analytics: spend, tokens, and request volume across all principals."
          data={data}
          status={status}
          error={error}
          days={days}
          granularity={granularity}
          periodPrefKey="analytics.defaultPeriod"
          onSetDays={setDays}
          onSetGranularity={setGranularity}
          filterSlot={filterSlot}
        />
      </TabsContent>

      <TabsContent value="principals">
        <AnalyticsPrincipalsTable key={days} days={days} />
      </TabsContent>
    </Tabs>
  );
}
