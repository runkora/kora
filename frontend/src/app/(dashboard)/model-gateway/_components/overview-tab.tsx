'use client';

import {
  AreaChart,
  Area,
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
  Grid,
  Heading,
  Stack,
  Text,
} from '@orbit/terra';
import { Activity, Zap, DollarSign, Clock, AlertCircle, HardDrive } from 'lucide-react';
import { DAILY_SPEND, DAILY_REQUESTS, STATS } from '../_data/mock';

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

// ── Requests Trend Chart ──────────────────────────────────────────────────────

function RequestsTrendChart() {
  const data = DAILY_REQUESTS.slice(-14);

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Requests — last 14 days</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={240}>
          <AreaChart data={data} margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
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
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={formatRequestsAxis}
              width={52}
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
              type="monotone"
              dataKey="total"
              stroke={COLORS.accent}
              fillOpacity={1}
              fill="url(#overviewColorTotal)"
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}

// ── Main Component ────────────────────────────────────────────────────────────

export function OverviewTab() {
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

        {/* Requests trend chart */}
        <RequestsTrendChart />
      </section>
    </Stack>
  );
}
