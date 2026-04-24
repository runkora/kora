'use client';

import {
  ComposedChart,
  Area,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';
import {
  Badge,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
  Grid,
  Heading,
  Icon,
  Progress,
  Separator,
  Stack,
  StatusDot,
  Text,
} from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing';
import Link from 'next/link';
import {
  Activity,
  AlertCircle,
  Clock,
  DollarSign,
  HardDrive,
  ScrollText,
  Server,
  TrendingUp,
  Zap,
} from 'lucide-react';
import { fmtCost, fmtNumCompact, fmtTime } from '@orbit/utils';
import type {
  AnalyticsBucket,
  AnalyticsCostByModel,
  AnalyticsKPIs,
  InferenceUsageRecord,
  ModelRow,
} from '../_types';

// ── Overview data shape ───────────────────────────────────────────────────────

export interface OverviewData {
  kpis: AnalyticsKPIs;
  buckets: AnalyticsBucket[]; // 30 daily buckets (fetched with days=30)
  costByModel: AnalyticsCostByModel[];
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmtPercent(n: number): string {
  return `${n.toFixed(1)}%`;
}

function formatDateAxis(dateStr: string): string {
  try {
    const d = new Date(dateStr + 'T00:00:00Z');
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  } catch {
    return dateStr;
  }
}

function formatRequestsAxis(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return n.toLocaleString();
}

function bucketSum(buckets: AnalyticsBucket[], key: keyof AnalyticsBucket): number {
  return buckets.reduce((s, b) => s + (b[key] as number), 0);
}

function pctDelta(curr: number, prev: number): number {
  return prev > 0 ? ((curr - prev) / prev) * 100 : 0;
}

// ── Chart Colors (Terra tokens as hex) ────────────────────────────────────────

const COLORS = {
  accent: '#e8a07a',
  healthy: '#9ce37d',
  degraded: '#ffd046',
  error: '#ef4444',
  surfaceOverlay: '#34343d',
  borderSubtle: '#44444d',
  foreground: '#ededef',
  subtle: '#8a8a94',
};

// ── KPI Card ──────────────────────────────────────────────────────────────────

interface KPICardProps {
  icon: React.ReactNode;
  label: string;
  value: string;
  deltaPercent?: number;
  deltaText?: string;
  positiveIsBad?: boolean;
}

function KPICard({ icon, label, value, deltaPercent, deltaText, positiveIsBad }: KPICardProps) {
  const isPositive = (deltaPercent ?? 0) >= 0;
  const isGood = positiveIsBad ? !isPositive : isPositive;
  const deltaColor = isGood ? 'status-healthy' : 'status-degraded';
  const deltaIcon = isPositive ? '▲' : '▼';

  return (
    <Card column>
      <CardHeader compact icon={icon}>
        <CardTitle>{label}</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          <Heading variant="metric">{value}</Heading>
          {deltaPercent !== undefined && deltaText !== undefined && (
            <Stack direction="horizontal" align="center" gap="tight">
              <Text size="caption" color={deltaColor}>
                {deltaIcon} {fmtPercent(Math.abs(deltaPercent))}
              </Text>
              <Text size="caption" color="subtle">
                {deltaText}
              </Text>
            </Stack>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

// ── Model Health Card ─────────────────────────────────────────────────────────

function ModelHealthCard({ models }: { models: ModelRow[] }) {
  const total = models.length;
  const available = models.filter((m) => m.status === 'available').length;
  const unavailable = total - available;
  const pct = total > 0 ? (available / total) * 100 : 0;

  const providerCounts = models.reduce<Record<string, number>>((acc, m) => {
    acc[m.owned_by] = (acc[m.owned_by] ?? 0) + 1;
    return acc;
  }, {});

  const modalityCounts = models.reduce<Record<string, number>>((acc, m) => {
    for (const mod of m.input_modalities) acc[mod] = (acc[mod] ?? 0) + 1;
    return acc;
  }, {});

  return (
    <Card column>
      <CardHeader compact icon={<Icon name="server" />}>
        <CardTitle>Models</CardTitle>
      </CardHeader>
      <CardContent>
        {total === 0 ? (
          <Text size="caption" color="subtle">
            No model data available
          </Text>
        ) : (
          <Stack gap="comfortable">
            {/* Headline + health bar */}
            <Stack gap="snug">
              <Stack direction="horizontal" align="baseline" gap="tight">
                <Heading variant="metric">{available}</Heading>
                <Text size="supporting" color="muted">
                  / {total} available
                </Text>
              </Stack>
              {/* Health bar */}
              <Progress
                label="Model health"
                value={pct}
                variant={unavailable > 0 ? 'degraded' : 'healthy'}
                size="sm"
              />
              {unavailable > 0 && (
                <Text size="caption" color="muted">
                  {unavailable} unavailable
                </Text>
              )}
            </Stack>

            <Separator />

            {/* Provider breakdown */}
            <Stack gap="snug">
              <Text size="caption" color="subtle" weight="emphasis">
                By provider
              </Text>
              <Stack gap="tight">
                {Object.entries(providerCounts).map(([provider, count]) => (
                  <Stack key={provider} direction="horizontal" align="center" justify="between">
                    <Text size="caption" color="muted">
                      {provider}
                    </Text>
                    <Text size="caption" weight="emphasis">
                      {count}
                    </Text>
                  </Stack>
                ))}
              </Stack>
            </Stack>

            {Object.keys(modalityCounts).length > 0 && (
              <>
                <Separator />
                <Stack gap="snug">
                  <Text size="caption" color="subtle" weight="emphasis">
                    By modality
                  </Text>
                  <Stack direction="horizontal" gap="snug" wrap>
                    {Object.entries(modalityCounts).map(([mod, count]) => (
                      <Stack key={mod} direction="horizontal" align="center" gap="tight">
                        <Text size="caption" weight="emphasis">
                          {count}
                        </Text>
                        <Text size="caption" color="muted">
                          {mod}
                        </Text>
                      </Stack>
                    ))}
                  </Stack>
                </Stack>
              </>
            )}
          </Stack>
        )}
      </CardContent>
      <CardFooter compact justify="between">
        <Text size="caption" color="subtle">
          {total} models registered
        </Text>
        <Link
          href={APP_ROUTES.modelGateway.models}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          View all →
        </Link>
      </CardFooter>
    </Card>
  );
}

// ── Requests & Errors Chart ───────────────────────────────────────────────────

function RequestsAndErrorsChart({ buckets }: { buckets: AnalyticsBucket[] }) {
  const data = buckets.map((b) => ({
    date: b.date,
    total: b.requests,
    errored: b.failed_requests,
  }));

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Requests &amp; Errors — last 14 days</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={240}>
          <ComposedChart data={data} margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
            <defs>
              <linearGradient id="overviewColorTotal" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={COLORS.accent} stopOpacity={0.3} />
                <stop offset="95%" stopColor={COLORS.accent} stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke={COLORS.borderSubtle} vertical={false} />
            <XAxis
              dataKey="date"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={formatDateAxis}
              minTickGap={40}
            />
            <YAxis
              yAxisId="left"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={formatRequestsAxis}
              width={52}
            />
            <YAxis
              yAxisId="right"
              orientation="right"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={formatRequestsAxis}
              width={40}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: COLORS.surfaceOverlay,
                border: `1px solid ${COLORS.borderSubtle}`,
                borderRadius: '6px',
                padding: '8px 12px',
              }}
              labelFormatter={(label: unknown) => formatDateAxis(String(label))}
              formatter={(value: unknown) => [formatRequestsAxis(Number(value)), '']}
              cursor={{ stroke: COLORS.borderSubtle, strokeWidth: 1 }}
            />
            <Area
              yAxisId="left"
              type="monotone"
              dataKey="total"
              stroke={COLORS.accent}
              fillOpacity={1}
              fill="url(#overviewColorTotal)"
              isAnimationActive={false}
            />
            <Line
              yAxisId="right"
              type="monotone"
              dataKey="errored"
              stroke={COLORS.error}
              strokeDasharray="3 2"
              dot={false}
              isAnimationActive={false}
            />
          </ComposedChart>
        </ResponsiveContainer>
        <Stack direction="horizontal" gap="comfortable" justify="center">
          <Stack direction="horizontal" align="center" gap="tight">
            <StatusDot status="accent" size="sm" aria-hidden />
            <Text size="caption" color="subtle">
              Total requests
            </Text>
          </Stack>
          <Stack direction="horizontal" align="center" gap="tight">
            <StatusDot status="error" size="sm" aria-hidden />
            <Text size="caption" color="subtle">
              Errors
            </Text>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}

// ── Top Models Card ───────────────────────────────────────────────────────────

function TopModelsCard({ costByModel }: { costByModel: AnalyticsCostByModel[] }) {
  const top5 = [...costByModel]
    .sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd)
    .slice(0, 5);
  const maxCost = top5.length > 0 ? top5[0].estimated_cost_usd : 1;

  return (
    <Card column>
      <CardHeader compact icon={<Icon name="trending-up" />}>
        <CardTitle>Top Models</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          {top5.length === 0 ? (
            <Text size="caption" color="subtle">
              No model usage in this period
            </Text>
          ) : (
            top5.map((entry) => (
              <Stack key={entry.model_id} gap="tight">
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="caption" mono truncate style={{ flex: 1, minWidth: 0 }}>
                    {entry.model_id}
                  </Text>
                  <Text
                    size="caption"
                    color="muted"
                    mono
                    style={{ fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}
                  >
                    {fmtCost(entry.estimated_cost_usd)}
                  </Text>
                </Stack>
                <span
                  style={{
                    flex: 1,
                    display: 'block',
                    height: 3,
                    borderRadius: 2,
                    background: COLORS.borderSubtle,
                    overflow: 'hidden',
                  }}
                >
                  <div
                    style={{
                      display: 'block',
                      height: '100%',
                      width: `${(entry.estimated_cost_usd / maxCost) * 100}%`,
                      background: COLORS.accent,
                      borderRadius: 2,
                    }}
                  />
                </span>
              </Stack>
            ))
          )}
        </Stack>
      </CardContent>
      <CardFooter compact justify="between">
        <Text size="caption" color="subtle">
          By spend, last 30 days
        </Text>
        <Link
          href={APP_ROUTES.modelGateway.analytics}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          View analytics →
        </Link>
      </CardFooter>
    </Card>
  );
}

// ── Recent Logs Card ──────────────────────────────────────────────────────────

function RecentLogsCard({ records }: { records: InferenceUsageRecord[] }) {
  return (
    <Card column>
      <CardHeader compact icon={<Icon name="scroll-text" />}>
        <CardTitle>Recent Requests</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="none">
          {records.length === 0 ? (
            <Text size="caption" color="subtle">
              No recent requests
            </Text>
          ) : (
            records.map((record, i) => {
              const isError = record.status !== 'success';
              const model = record.virtual_model_id ?? record.model_id;
              return (
                <Stack key={record.id} gap="none">
                  <Stack
                    direction="horizontal"
                    align="center"
                    gap="snug"
                    style={{
                      paddingBlock: '7px',
                      paddingInline: '6px',
                      marginInline: '-6px',
                      borderRadius: '4px',
                      background: isError ? 'rgba(239,68,68,0.06)' : 'transparent',
                    }}
                  >
                    <span
                      style={{
                        width: 7,
                        height: 7,
                        borderRadius: '50%',
                        flexShrink: 0,
                        background: isError ? COLORS.error : COLORS.healthy,
                        boxShadow: isError ? `0 0 0 2px rgba(239,68,68,0.2)` : undefined,
                      }}
                    />
                    <Text
                      size="caption"
                      mono
                      style={{
                        flex: 1,
                        minWidth: 0,
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {model}
                    </Text>
                    <Text
                      size="caption"
                      color={isError ? 'muted' : 'subtle'}
                      mono
                      style={{ flexShrink: 0 }}
                    >
                      {record.latency_ms >= 1000
                        ? `${(record.latency_ms / 1000).toFixed(1)}s`
                        : `${record.latency_ms}ms`}
                    </Text>
                    <Text
                      size="caption"
                      color="subtle"
                      style={{ flexShrink: 0, minWidth: '44px', textAlign: 'right' }}
                    >
                      {fmtTime(record.created_at)}
                    </Text>
                  </Stack>
                  {i < records.length - 1 && <Separator />}
                </Stack>
              );
            })
          )}
        </Stack>
      </CardContent>
      <CardFooter compact justify="between">
        <Text size="caption" color="subtle">
          Last {records.length} requests
        </Text>
        <Link
          href={APP_ROUTES.modelGateway.logs}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          View all logs →
        </Link>
      </CardFooter>
    </Card>
  );
}

// ── Main Component ────────────────────────────────────────────────────────────

interface OverviewTabProps {
  models: ModelRow[];
  data: OverviewData | null;
  recentRecords: InferenceUsageRecord[];
}

export function OverviewTab({ models, data, recentRecords }: OverviewTabProps) {
  const curr7 = data?.buckets.slice(-7) ?? [];
  const prev7 = data?.buckets.slice(-14, -7) ?? [];

  const currRequests = bucketSum(curr7, 'requests');
  const prevRequests = bucketSum(prev7, 'requests');
  const currTokens =
    bucketSum(curr7, 'prompt_tokens') +
    bucketSum(curr7, 'completion_tokens') +
    bucketSum(curr7, 'cache_read_tokens');
  const prevTokens =
    bucketSum(prev7, 'prompt_tokens') +
    bucketSum(prev7, 'completion_tokens') +
    bucketSum(prev7, 'cache_read_tokens');
  const currCost = bucketSum(curr7, 'estimated_cost_usd');
  const prevCost = bucketSum(prev7, 'estimated_cost_usd');
  const currErrors = bucketSum(curr7, 'failed_requests');
  const prevErrors = bucketSum(prev7, 'failed_requests');
  const currErrorRate = currRequests > 0 ? (currErrors / currRequests) * 100 : 0;
  const prevErrorRate = prevRequests > 0 ? (prevErrors / prevRequests) * 100 : 0;

  const errorRateBadge = data !== null ? fmtPercent(currErrorRate) : '—';
  const cacheHitRateBadge = data !== null ? fmtPercent((data.kpis.cache_hit_rate ?? 0) * 100) : '—';

  const isOperational = data === null || (data.kpis.error_rate ?? 0) < 0.1;

  return (
    <Stack direction="vertical" gap="comfortable" asChild>
      <section>
        {/* Page header */}
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" gap="snug">
            <Heading variant="title">Model Gateway</Heading>
            <Badge variant={isOperational ? 'healthy' : 'degraded'} dot size="sm">
              {isOperational ? 'Operational' : 'Degraded'}
            </Badge>
          </Stack>
          <Text size="supporting" color="muted">
            Unified inference proxy — route, observe, and govern all LLM traffic from a single
            control plane.
          </Text>
        </Stack>
        <Text size="supporting" color="muted">
          Unified inference proxy — route, observe, and govern all LLM traffic from a single control
          plane.
        </Text>
      </Stack>

        {/* KPI Cards */}
        <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="comfortable">
          <KPICard
            icon={<Icon name="activity" />}
            label="Total Requests"
            value={data ? fmtNumCompact(currRequests) : '—'}
            deltaPercent={data ? pctDelta(currRequests, prevRequests) : undefined}
            deltaText="vs prior 7 days"
          />
          <KPICard
            icon={<Icon name="zap" />}
            label="Total Tokens"
            value={data ? fmtNumCompact(currTokens) : '—'}
            deltaPercent={data ? pctDelta(currTokens, prevTokens) : undefined}
            deltaText="vs prior 7 days"
          />
          <KPICard
            icon={<Icon name="dollar-sign" />}
            label="Total Spend"
            value={data ? fmtCost(currCost) : '—'}
            deltaPercent={data ? pctDelta(currCost, prevCost) : undefined}
            deltaText="vs prior 7 days"
            positiveIsBad
          />
          <KPICard
            icon={<Icon name="clock" />}
            label="Avg Latency"
            value={data ? `${Math.round(data.kpis.avg_latency_ms)}ms` : '—'}
          />
          <KPICard
            icon={<Icon name="alert-circle" />}
            label="Error Rate"
            value={errorRateBadge}
            deltaPercent={data ? pctDelta(currErrorRate, prevErrorRate) : undefined}
            deltaText="vs prior 7 days"
            positiveIsBad
          />
          <KPICard
            icon={<Icon name="hard-drive" />}
            label="Cache Hit Rate"
            value={cacheHitRateBadge}
          />
        </Grid>

        {/* Requests & errors chart */}
        <RequestsAndErrorsChart buckets={data?.buckets.slice(-14) ?? []} />

        {/* Three-column grid: model status + top models + recent logs */}
        <Grid cols={{ base: 1, lg: 3 }} gap="comfortable">
          <ModelHealthCard models={models} />
          <TopModelsCard costByModel={data?.costByModel ?? []} />
          <RecentLogsCard records={recentRecords} />
        </Grid>
      </section>
    </Stack>
  );
}
