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
  Separator,
  Stack,
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
import {
  DAILY_REQUESTS,
  DAILY_SPEND,
  RECENT_LOGS,
  STATS,
  TOP_MODELS_BY_USAGE,
} from '../_data/mock';
import type { ModelRow } from '../_types';

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmt(usd: number): string {
  return `$${usd.toFixed(2)}`;
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return n.toLocaleString();
}

function fmtReqs(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

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
  deltaPercent: number;
  deltaText: string;
  positiveIsBad?: boolean;
}

function KPICard({ icon, label, value, deltaPercent, deltaText, positiveIsBad }: KPICardProps) {
  const isPositive = deltaPercent >= 0;
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
          <Stack direction="horizontal" align="center" gap="tight">
            <Text size="caption" color={deltaColor}>
              {deltaIcon} {fmtPercent(Math.abs(deltaPercent))}
            </Text>
            <Text size="caption" color="subtle">
              {deltaText}
            </Text>
          </Stack>
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
      <CardHeader compact icon={<Server className="size-4" />}>
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
              <span
                style={{
                  display: 'block',
                  height: 4,
                  borderRadius: 2,
                  background: COLORS.borderSubtle,
                  overflow: 'hidden',
                }}
              >
                <span
                  style={{
                    display: 'block',
                    height: '100%',
                    width: `${pct}%`,
                    background: unavailable > 0 ? COLORS.degraded : COLORS.healthy,
                    borderRadius: 2,
                    transition: 'width 0.3s',
                  }}
                />
              </span>
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
                  <Stack direction="horizontal" gap="snug" style={{ flexWrap: 'wrap' }}>
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

function RequestsAndErrorsChart() {
  const data = DAILY_REQUESTS.slice(-14);

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
            <span
              style={{
                width: 8,
                height: 8,
                borderRadius: '50%',
                background: COLORS.accent,
                display: 'inline-block',
              }}
            />
            <Text size="caption" color="subtle">
              Total requests
            </Text>
          </Stack>
          <Stack direction="horizontal" align="center" gap="tight">
            <span
              style={{
                width: 8,
                height: 8,
                borderRadius: '50%',
                background: COLORS.error,
                display: 'inline-block',
              }}
            />
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

function TopModelsCard() {
  const maxRequests =
    TOP_MODELS_BY_USAGE.length > 0 ? Math.max(...TOP_MODELS_BY_USAGE.map((e) => e.requests)) : 1;

  return (
    <Card column>
      <CardHeader compact icon={<TrendingUp className="size-4" />}>
        <CardTitle>Top Models</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          {TOP_MODELS_BY_USAGE.map((entry) => (
            <Stack key={entry.model} gap="tight">
              <Stack direction="horizontal" align="center" justify="between">
                <Text size="caption" mono truncate style={{ flex: 1, minWidth: 0 }}>
                  {entry.model}
                </Text>
                <Text
                  size="caption"
                  color="muted"
                  mono
                  style={{ fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}
                >
                  {fmt(entry.cost)}
                </Text>
              </Stack>
              {/* Relative usage bar */}
              <Stack direction="horizontal" align="center" gap="tight">
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
                  <span
                    style={{
                      display: 'block',
                      height: '100%',
                      width: `${(entry.requests / maxRequests) * 100}%`,
                      background: COLORS.accent,
                      borderRadius: 2,
                    }}
                  />
                </span>
                <Text
                  size="caption"
                  color="subtle"
                  style={{ flexShrink: 0, minWidth: '36px', textAlign: 'right' }}
                >
                  {fmtReqs(entry.requests)}
                </Text>
              </Stack>
            </Stack>
          ))}
        </Stack>
      </CardContent>
      <CardFooter compact justify="between">
        <Text size="caption" color="subtle">
          By spend, last 30 days
        </Text>
        <Link
          href={APP_ROUTES.modelGateway.myUsage}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          View usage →
        </Link>
      </CardFooter>
    </Card>
  );
}

// ── Recent Logs Card ──────────────────────────────────────────────────────────

function RecentLogsCard() {
  return (
    <Card column>
      <CardHeader compact icon={<ScrollText className="size-4" />}>
        <CardTitle>Recent Requests</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="none">
          {RECENT_LOGS.map((entry, i) => {
            const isError = entry.status === 'error';
            return (
              <Stack key={entry.id} gap="none">
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
                  {/* Status indicator */}
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
                  {/* Model */}
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
                    {entry.model}
                  </Text>
                  {/* Latency */}
                  <Text
                    size="caption"
                    color={isError ? 'muted' : 'subtle'}
                    mono
                    style={{ flexShrink: 0 }}
                  >
                    {entry.latencyMs >= 1000
                      ? `${(entry.latencyMs / 1000).toFixed(1)}s`
                      : `${entry.latencyMs}ms`}
                  </Text>
                  {/* Time */}
                  <Text
                    size="caption"
                    color="subtle"
                    style={{ flexShrink: 0, minWidth: '44px', textAlign: 'right' }}
                  >
                    {entry.minsAgo === 0 ? 'just now' : `${entry.minsAgo}m ago`}
                  </Text>
                </Stack>
                {i < RECENT_LOGS.length - 1 && <Separator />}
              </Stack>
            );
          })}
        </Stack>
      </CardContent>
      <CardFooter compact justify="between">
        <Text size="caption" color="subtle">
          Last {RECENT_LOGS.length} requests
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
}

export function OverviewTab({ models }: OverviewTabProps) {
  // Derive DAILY_TOKENS inline for delta calculations
  const dailyTokens = DAILY_SPEND.map((d) => ({
    date: d.date,
    totalTokens: Math.round(d.promptUsd * 140_000) + Math.round(d.outputUsd * 14_000),
  }));

  // 7-day deltas (current vs previous 7 days)
  const currentWeek = DAILY_REQUESTS.slice(-7);
  const previousWeek = DAILY_REQUESTS.slice(-14, -7);

  const currentWeekTotal = currentWeek.reduce((s, r) => s + r.total, 0);
  const previousWeekTotal = previousWeek.reduce((s, r) => s + r.total, 0);
  const requestsDelta =
    previousWeekTotal > 0 ? ((currentWeekTotal - previousWeekTotal) / previousWeekTotal) * 100 : 0;

  const currentWeekTokens = dailyTokens.slice(-7).reduce((s, r) => s + r.totalTokens, 0);
  const previousWeekTokens = dailyTokens.slice(-14, -7).reduce((s, r) => s + r.totalTokens, 0);
  const tokensDelta =
    previousWeekTokens > 0
      ? ((currentWeekTokens - previousWeekTokens) / previousWeekTokens) * 100
      : 0;

  const currentWeekSpend = DAILY_SPEND.slice(-7).reduce((s, r) => s + r.totalUsd, 0);
  const previousWeekSpend = DAILY_SPEND.slice(-14, -7).reduce((s, r) => s + r.totalUsd, 0);
  const spendDelta =
    previousWeekSpend > 0 ? ((currentWeekSpend - previousWeekSpend) / previousWeekSpend) * 100 : 0;

  return (
    <Stack direction="vertical" gap="comfortable" asChild>
      <section>
        {/* Page header */}
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" gap="snug">
            <Heading variant="title">Model Gateway</Heading>
            <Badge variant="healthy" dot size="sm">
              Operational
            </Badge>
          </Stack>
          <Text size="supporting" color="muted">
            Unified inference proxy — route, observe, and govern all LLM traffic from a single
            control plane.
          </Text>
        </Stack>

        {/* KPI Cards */}
        <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="comfortable">
          <KPICard
            icon={<Activity className="size-4" />}
            label="Total Requests"
            value={fmtReqs(STATS.requests.total)}
            deltaPercent={requestsDelta}
            deltaText="vs last 7 days"
          />
          <KPICard
            icon={<Zap className="size-4" />}
            label="Total Tokens"
            value={fmtTokens(STATS.tokens.period)}
            deltaPercent={tokensDelta}
            deltaText="vs last 7 days"
          />
          <KPICard
            icon={<DollarSign className="size-4" />}
            label="Total Spend"
            value={fmt(STATS.spend.period)}
            deltaPercent={spendDelta}
            deltaText="vs last 7 days"
            positiveIsBad
          />
          <KPICard
            icon={<Clock className="size-4" />}
            label="Avg Latency"
            value="142ms"
            deltaPercent={2.3}
            deltaText="vs last 7 days"
            positiveIsBad
          />
          <KPICard
            icon={<AlertCircle className="size-4" />}
            label="Error Rate"
            value={fmtPercent((STATS.requests.errored / STATS.requests.total) * 100)}
            deltaPercent={-0.8}
            deltaText="vs last 7 days"
            positiveIsBad
          />
          <KPICard
            icon={<HardDrive className="size-4" />}
            label="Cache Hit Rate"
            value="68.2%"
            deltaPercent={5.1}
            deltaText="vs last 7 days"
          />
        </Grid>

        {/* Requests & errors chart */}
        <RequestsAndErrorsChart />

        {/* Three-column grid: model status + top models + recent logs */}
        <Grid cols={{ base: 1, lg: 3 }} gap="comfortable">
          <ModelHealthCard models={models} />
          <TopModelsCard />
          <RecentLogsCard />
        </Grid>
      </section>
    </Stack>
  );
}
